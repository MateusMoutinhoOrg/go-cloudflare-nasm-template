"""Code shared by all the scripts in this folder.

What is here, in this order:

  - project:       where things are (ROOT, ENGINE, ...)
  - messages:      step, ok, fail
  - command line:  new_parser, port_number, script_command
  - commands:      run, output, run_script
  - tools:         tools_env, require_dependencies (Go, Node.js, Wrangler)
  - repository:    owner_options, git, current_commit
  - server:        responds, wait_until_responds
  - systemd:       require_systemd, require_root, lock, systemctl, unit names
  - installation:  read_state, write_state, remove_instances, remove_watcher
"""

import argparse
import fcntl
import http.client
import json
import os
import pwd
import re
import shlex
import shutil
import subprocess
import sys
import time
from pathlib import Path

if sys.version_info < (3, 9):
    sys.exit("These scripts need Python 3.9 or newer.")

# Never crash because a terminal cannot print "✔".
sys.stdout.reconfigure(errors="replace")
sys.stderr.reconfigure(errors="replace")


# ------------------------------------------------------------------ project

# The project root is found from the location of this file, so the scripts
# work no matter which directory they are called from.
ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = ROOT / "scripts"
ENGINE = ROOT / "engine"
TOOLS = ENGINE / "tools"  # Go and Node.js downloaded by install.py
WRANGLER = ENGINE / "node_modules" / ".bin" / "wrangler"

GO_MIN = "1.21"
NODE_MIN = "22"  # required by Wrangler 4


# ----------------------------------------------------------------- messages

def _color(code, text):
    return f"\033[{code}m{text}\033[0m" if sys.stdout.isatty() else text


def step(text):
    """Announces the next thing the script will do."""
    print("\n" + _color(1, f"==> {text}"), flush=True)


def ok(text):
    print(f"  {_color(32, '✔')} {text}", flush=True)


def fail(text):
    """Prints the error and ends the script with exit code 1."""
    sys.stdout.flush()
    print(f"✘ {text}", file=sys.stderr, flush=True)
    sys.exit(1)


def script_command(name, *arguments):
    """The command line that runs another script of this folder (for messages)."""
    return shlex.join(["python3", os.path.relpath(SCRIPTS / name), *arguments])


def new_parser(description):
    """The command-line parser every script starts with (gives --help, rejects unknown flags)."""
    return argparse.ArgumentParser(description=description,
                                   formatter_class=argparse.RawDescriptionHelpFormatter)


def port_number(text):
    """Converts a command-line value to a port number (argparse reports invalid ones)."""
    number = int(text)
    if not 1 <= number <= 65535:
        raise ValueError(text)
    return number


# ----------------------------------------------------------------- commands

def run(command, **options):
    """Runs a command showing its output. Returns True if it succeeded."""
    sys.stdout.flush()
    try:
        return subprocess.run([str(part) for part in command], **options).returncode == 0
    except OSError:  # e.g. the program does not exist
        return False


def output(command, **options):
    """Runs a command quietly and returns what it printed, or None if it failed."""
    try:
        result = subprocess.run([str(part) for part in command], text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, **options)
    except OSError:
        return None
    return result.stdout.strip() if result.returncode == 0 else None


def run_script(name, *arguments, **options):
    """Runs another script of this folder as a new process. Returns True if it succeeded."""
    return run([sys.executable, SCRIPTS / name, *arguments], **options)


# -------------------------------------------------------------------- tools

# Where Go, Node.js and Wrangler are looked up, in this order: first what
# install.py put inside engine/, then the usual system directories.
#
# The PATH of whoever calls the script is NOT used to find them: sudo and
# systemd have a different PATH, and every script must agree on which tools
# are installed.
TOOL_DIRS = [
    ENGINE / "node_modules" / ".bin",
    TOOLS / "node" / "bin",
    TOOLS / "go" / "bin",
    "/usr/local/go/bin",
    "/opt/homebrew/bin",
    "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin",
]


def tools_env():
    """A copy of the environment with the tool directories first in the PATH."""
    env = dict(os.environ)
    env["PATH"] = os.pathsep.join([*map(str, TOOL_DIRS), env.get("PATH", "")])
    return env


def find_tool(name):
    """Full path of a tool found in TOOL_DIRS, or None."""
    return shutil.which(name, path=os.pathsep.join(map(str, TOOL_DIRS)))


def tool_version(*command):
    """Runs e.g. `node --version` and returns the version it prints ("24.1.0"), or None."""
    program = find_tool(command[0])
    text = program and output([program, *command[1:]], env=tools_env())
    version = re.search(r"\d+(\.\d+)+", text or "")
    return version.group() if version else None


def version_at_least(current, minimum):
    """True if `current` >= `minimum` (e.g. "1.26.0" >= "1.21"). False if there is no version."""
    def numbers(version):
        return [int(number) for number in version.split(".")]
    return bool(current) and numbers(current) >= numbers(minimum)


def go_version():
    return tool_version("go", "env", "GOVERSION")


def node_version():
    return tool_version("node", "--version") if find_tool("npm") else None


def missing_dependencies():
    """Names of the dependencies that install.py still has to install."""
    missing = []
    if not version_at_least(go_version(), GO_MIN):
        missing.append(f"Go >= {GO_MIN}")
    if not version_at_least(node_version(), NODE_MIN):
        missing.append(f"Node.js >= {NODE_MIN}")
    if not WRANGLER.exists():
        missing.append("Wrangler")
    return missing


def require_dependencies():
    missing = missing_dependencies()
    if missing:
        fail(f"Dependencies not installed ({', '.join(missing)}). "
             f"Run first: {script_command('install.py')}")


# --------------------------------------------------------------- repository

def refuse_sudo():
    """For the scripts that must NOT run as root (install, test, deploy)."""
    if os.geteuid() == 0 and ROOT.stat().st_uid != 0:
        fail("Do not run this script with sudo: it would leave files owned by root in the project.")


def repo_owner():
    """The user who owns the project directory (an entry of the password database)."""
    try:
        return pwd.getpwuid(ROOT.stat().st_uid)
    except KeyError:
        fail(f"The owner of {ROOT} is not a user of this machine.")


def owner_options():
    """subprocess options that make a command run in the project, as the project owner.

    When we are root (sudo), the command drops to the owner of the project
    directory, so nothing owned by root is left in the project. When we are
    not root, the command simply runs as the current user.
    """
    options = {"cwd": ROOT, "env": tools_env()}
    if os.geteuid() == 0:
        owner = repo_owner()
        options["env"].update(HOME=owner.pw_dir, USER=owner.pw_name, LOGNAME=owner.pw_name)
        options.update(user=owner.pw_uid, group=owner.pw_gid,
                       extra_groups=os.getgrouplist(owner.pw_name, owner.pw_gid))
    return options


def git(*arguments, capture=False):
    """Runs git in the project, as its owner, in a way that can never ask for a password.

    This is how the watcher runs git (no terminal, no ssh-agent), so a command
    that works here also works there.
    """
    options = owner_options()
    options["env"]["GIT_TERMINAL_PROMPT"] = "0"
    for variable in ("SSH_AUTH_SOCK", "SSH_ASKPASS", "GIT_ASKPASS"):
        options["env"].pop(variable, None)
    options.update(stdin=subprocess.DEVNULL, start_new_session=True)  # no terminal to ask on
    command = ["git", *arguments]
    return output(command, **options) if capture else run(command, **options)


def current_commit():
    """The commit the repository is on, or None if it cannot be read."""
    return git("rev-parse", "HEAD", capture=True)


# ------------------------------------------------------------------- server

def responds(port):
    """True if the server on this port answers GET /.

    Any answer counts (even 404: the sandbox may have no "/" route), except
    a server error (5xx), which means the server is up but broken.
    """
    connection = http.client.HTTPConnection("localhost", port, timeout=5)
    try:
        connection.request("GET", "/")
        return connection.getresponse().status < 500
    except (OSError, http.client.HTTPException):  # refused, timed out...
        return False
    finally:
        connection.close()


def wait_until_responds(ports, seconds=60, gave_up=lambda port: False):
    """Waits until every port answers. Returns the ports that never did.

    A silent port is not waited for any longer once gave_up(port) tells that
    it will never answer; the others still get their time.
    """
    deadline = time.monotonic() + seconds
    silent = list(ports)
    while True:
        silent = [port for port in silent if not responds(port)]
        still_starting = [port for port in silent if not gave_up(port)]
        if not still_starting or time.monotonic() >= deadline:
            return silent
        time.sleep(1)


# ------------------------------------------------------------------ systemd

NAME = "go-cloudflare-nasm-template"  # every systemd unit starts with this name (see rename.py)

UNIT_DIR = Path("/etc/systemd/system")
TEMPLATE_UNIT = UNIT_DIR / f"{NAME}@.service"  # one instance per port: ...@3000.service
WATCHER_SERVICE = UNIT_DIR / f"{NAME}-watcher.service"
WATCHER_TIMER = UNIT_DIR / f"{NAME}-watcher.timer"

STATE_FILE = Path(f"/etc/{NAME}/install.json")
LOCK_FILE = Path(f"/run/{NAME}.lock")
LOCK_HELD = "SERVER_SCRIPTS_LOCK_HELD"  # environment variable, see lock()
_lock_file = None

INSTANCES_LOG = f"sudo journalctl -u '{NAME}@*' -f"
WATCHER_LOG = f"sudo journalctl -u {NAME}-watcher -f"


def instance_unit(port):
    return f"{NAME}@{port}.service"


def require_systemd():
    on_systemd = sys.platform.startswith("linux") and Path("/run/systemd/system").is_dir()
    if not (on_systemd and shutil.which("systemctl")):
        fail("This script needs Linux with systemd.")


def require_root():
    if os.geteuid() != 0:
        fail(f"This script needs root. Run: sudo {shlex.join(['python3', *sys.argv])}")


def lock():
    """Makes sure only one script changes systemd at a time.

    The lock is released by the operating system when the script ends.
    A script started by another one that already holds the lock (update.py
    starts system-ctl-install.py) does not take it again.
    """
    global _lock_file
    if os.environ.get(LOCK_HELD):
        return
    _lock_file = open(LOCK_FILE, "w")  # stays open (and locked) until the script ends
    try:
        fcntl.flock(_lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        fail("Another script is changing systemd right now. Try again when it finishes.")
    os.environ[LOCK_HELD] = "1"  # inherited by the scripts we start


def systemctl(*arguments):
    """Runs systemctl and ends the script if it fails."""
    if not run(["systemctl", *arguments]):
        fail(f"Failed: systemctl {shlex.join(arguments)}")


def unit_properties(unit, *names):
    """Reads properties of a unit (e.g. ActiveState, Result) into a dictionary."""
    text = output(["systemctl", "show", unit, "--property=" + ",".join(names)]) or ""
    return dict(line.split("=", 1) for line in text.splitlines() if "=" in line)


def unit_path(path):
    """A path written the way systemd unit files expect (quoted, so spaces are fine)."""
    if any(character in str(path) for character in '"\\%$\n'):
        fail(f"systemd unit files cannot use this path: {path}")
    return f'"{path}"'


def write_unit(path, text):
    path.write_text(text)
    path.chmod(0o644)


# ------------------------------------------------------------- installation

def read_state():
    """What system-ctl-install.py installed (ports, restart, user, commit), or None."""
    try:
        return json.loads(STATE_FILE.read_text())
    except FileNotFoundError:
        return None
    except ValueError:
        fail(f"{STATE_FILE} is corrupted. Reinstall with: sudo {script_command('system-ctl-install.py', '--port', '...')}")


def write_state(state):
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    temporary = STATE_FILE.with_suffix(".tmp")
    temporary.write_text(json.dumps(state, indent=2) + "\n")
    temporary.chmod(0o644)  # status.py reads it without sudo
    temporary.replace(STATE_FILE)  # all or nothing: never a half-written file


def require_installed():
    """Returns the state of the installation; ends the script if there is none."""
    state = read_state()
    if state is None:
        fail("The server is not installed in systemd. Run first: "
             f"sudo {script_command('system-ctl-install.py', '--port', '3000:3005')}")
    return state


def instance_units():
    """Every instance systemd knows about: running, stopped or just enabled at boot."""
    listing = output(["systemctl", "list-units", "--all", "--plain", "--no-legend",
                      f"{NAME}@*.service"]) or ""
    units = {line.split()[0] for line in listing.splitlines() if line.strip()}
    units |= {link.name for link in UNIT_DIR.glob(f"*.wants/{NAME}@*.service")}
    return sorted(units)


def forget_failures(*units):
    """Clears the "failed" mark systemd keeps on units that crashed."""
    run(["systemctl", "reset-failed", *units], stderr=subprocess.DEVNULL)  # fine if none failed


def remove_instances():
    """Stops and disables every instance of the server. Returns how many there were."""
    forget_failures(f"{NAME}@*.service")
    units = instance_units()
    if units:
        systemctl("disable", "--quiet", "--now", *units)
        forget_failures(f"{NAME}@*.service")
    return len(units)


def remove_watcher():
    """Stops, disables and deletes the watcher. Returns False if there was none."""
    if not WATCHER_TIMER.exists() and not WATCHER_SERVICE.exists():
        return False
    if WATCHER_TIMER.exists():
        systemctl("disable", "--quiet", "--now", WATCHER_TIMER.name)
    if WATCHER_SERVICE.exists():
        systemctl("stop", WATCHER_SERVICE.name)
    WATCHER_TIMER.unlink(missing_ok=True)
    WATCHER_SERVICE.unlink(missing_ok=True)
    systemctl("daemon-reload")
    forget_failures(WATCHER_TIMER.name, WATCHER_SERVICE.name)
    return True

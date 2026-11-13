#!/usr/bin/env python3
"""Renames the project. Run it once, right after you fork the template.

What changes:
  - the Go module: go.mod and the imports of every .go file
  - the name of the Worker: wrangler.jsonc
  - the name of the systemd services: scripts/common.py

Without arguments, the new name is the repository you cloned (the git remote
"origin"), so in a fork this is all it takes:

  python3 scripts/rename.py

Rename before you deploy: for Cloudflare, and for the local server, another
name is another Worker, with empty databases.

examples:
  python3 scripts/rename.py github.com/ana/my-server
  python3 scripts/rename.py github.com/ana/my-server --name api
"""

import os
import re
import sys
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ENGINE, ROOT, SCRIPTS, TOOLS, fail, ok, step

# Where each name is written: the file, and the text that comes right before
# the name in it.
GO_MODULE = (ROOT / "go.mod", r"^module\s+", r"\S+")
WORKER_NAME = (ROOT / "wrangler.jsonc", r'^\s*"name":\s*"', r'[^"]*')
SYSTEMD_NAME = (SCRIPTS / "common.py", r'^NAME = "', r'[^"]*')


def main():
    args = parse_args()
    common.refuse_sudo()

    module = module_path(args.module or origin_address())
    name = args.name or default_name(module)
    check_names(module, name)

    old_module, old_name = current(GO_MODULE), current(WORKER_NAME)
    if (module, name, name) == (old_module, old_name, common.NAME):
        return ok(f"nothing to rename: the project is already {module} (name: {name})")
    if name != common.NAME:
        refuse_if_installed()

    step("Renaming the project")
    # Everything is prepared first and written at the end, so an error never
    # leaves the project half renamed.
    changes = {
        GO_MODULE[0]: renamed(GO_MODULE, module),
        WORKER_NAME[0]: renamed(WORKER_NAME, name),
        SYSTEMD_NAME[0]: renamed(SYSTEMD_NAME, name),
    }
    for file in go_files():
        changes[file] = (read(file).replace(f'"{old_module}/', f'"{module}/')
                                   .replace(f'"{old_module}"', f'"{module}"'))
    changed = [file for file, text in changes.items() if text != read(file)]
    for file in changed:
        file.write_bytes(changes[file].encode("utf-8"))

    if module != old_module:
        ok(f"Go module: {old_module} -> {module}")
    if name != old_name:
        ok(f"Worker and systemd services: {old_name} -> {name}")
    ok(f"{len(changed)} file(s) changed: " + ", ".join(str(file.relative_to(ROOT)) for file in changed))

    step("Done! See what changed with `git diff`, then commit it.")
    next_script = "install.py" if common.missing_dependencies() else "test.py"
    print(f"  Next: {common.script_command(next_script)}")


def parse_args():
    parser = common.new_parser(__doc__)
    parser.add_argument("module", nargs="?", metavar="MODULE",
                        help="the new Go module, or the address of your repository: "
                             "github.com/ana/my-server (default: the git remote origin)")
    parser.add_argument("--name", metavar="NAME",
                        help="name of the Worker and of the systemd services "
                             "(default: the last part of the module)")
    return parser.parse_args()


# -------------------------------------------------------------- the new names

def origin_address():
    """The address this repository was cloned from. Ends the script if there is none."""
    address = common.git("remote", "get-url", "origin", capture=True)
    if not address:
        fail("This repository has no git remote called origin. Say the new name:\n"
             f"  {common.script_command('rename.py', 'github.com/YOUR-USER/YOUR-REPOSITORY')}")
    return address


def module_path(address):
    """Converts the address of a repository into a Go module path.

    "https://github.com/ana/my-server.git", "git@github.com:ana/my-server.git"
    and "github.com/ana/my-server" all become "github.com/ana/my-server".
    """
    path = address.strip()
    path = re.sub(r"^[a-z+]+://", "", path)            # https://  ssh://
    path = re.sub(r"^[^/]*@", "", path)                # git@  user:token@
    path = re.sub(r"^([^/:]+):(\d+/)?", r"\1/", path)  # host:owner and host:22/owner -> host/owner
    return re.sub(r"(\.git)?/*$", "", path)


def default_name(module):
    """The last part of the module, written the way Cloudflare accepts: "My_Server" -> "my-server"."""
    return re.sub(r"[^a-z0-9]+", "-", module.rsplit("/", 1)[-1].lower()).strip("-")


def check_names(module, name):
    if not re.fullmatch(r"[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*", module):
        fail(f"{module!r} is not a valid Go module. Say the new name, like this:\n"
             f"  {common.script_command('rename.py', 'github.com/YOUR-USER/YOUR-REPOSITORY')}")
    if not re.fullmatch(r"[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?", name):
        fail(f"{name!r} is not a valid name for the Worker: use lowercase letters, digits\n"
             "and dashes, 63 characters at most. Choose one with --name:\n"
             f"  {common.script_command('rename.py', module, '--name', 'my-server')}")


def refuse_if_installed():
    """The systemd units carry the current name: renaming now would leave them behind."""
    units = [common.STATE_FILE, common.TEMPLATE_UNIT, common.WATCHER_SERVICE, common.WATCHER_TIMER]
    if any(unit.exists() for unit in units):
        fail(f"The server is installed in systemd with the current name ({common.NAME}).\n"
             "Remove it first, and install it again after renaming:\n"
             f"  sudo {common.script_command('system-ctl-remove.py')}")


# ------------------------------------------------------------------ the files

def read(file):
    return file.read_bytes().decode("utf-8")  # as bytes, so line endings stay as they are


def find(place):
    """Finds a name in its file. Returns the content of the file and where the name is."""
    file, before, name = place
    content = read(file)
    found = re.search(f"(?m)(?:{before})({name})", content)
    if found is None:
        fail(f"Could not find the name to change in {file.relative_to(ROOT)}.")
    return content, found.span(1)


def current(place):
    """The name written in a file today."""
    content, (start, end) = find(place)
    return content[start:end]


def renamed(place, new):
    """The content of a file with its name replaced by the new one."""
    content, (start, end) = find(place)
    return content[:start] + new + content[end:]


def go_files():
    """Every .go file of the project (not the ones of the tools install.py downloads)."""
    generated = {TOOLS, ENGINE / "node_modules", ENGINE / "build"}
    for folder, subfolders, files in os.walk(ROOT):
        subfolders[:] = [name for name in subfolders
                         if not name.startswith(".") and Path(folder, name) not in generated]
        yield from (Path(folder, name) for name in files if name.endswith(".go"))


if __name__ == "__main__":
    main()

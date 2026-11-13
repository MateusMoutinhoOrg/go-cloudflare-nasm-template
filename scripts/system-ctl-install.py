#!/usr/bin/env python3
"""Registers the server in systemd: one service per port, started now and at every boot.

Each service runs scripts/test.py fixed on its own port, as the user who
called sudo (not as root). Installing again replaces the previous installation.

examples:
  sudo python3 scripts/system-ctl-install.py --port 3000
  sudo python3 scripts/system-ctl-install.py --port 3000:3005 --restart
"""

import os
import pwd
import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import NAME, fail, ok, step


def main():
    args = parse_args()
    common.require_systemd()
    common.require_root()
    common.require_dependencies()
    user = args.user or calling_user()
    if not user_exists(user):
        fail(f"The user {user} does not exist.")
    common.lock()

    ports = args.port
    units = [common.instance_unit(port) for port in ports]
    commit = common.current_commit()

    step(f"Installing {len(ports)} instance(s) in systemd, running as {user}")
    replaced = common.remove_instances()
    if replaced:
        ok(f"removed the previous installation ({replaced} instance(s))")
    common.write_unit(common.TEMPLATE_UNIT, template_unit(user, args.restart))
    common.systemctl("daemon-reload")
    # The commit is only recorded at the end, when everything works. If
    # something fails before that, update.py notices and installs again.
    state = {"ports": ports, "restart": args.restart, "user": user, "commit": None}
    common.write_state(state)
    common.systemctl("enable", "--quiet", "--now", *units)
    ok(f"enabled at boot and started: ports {ports[0]} to {ports[-1]}")

    # The instances start at the same time and share the processor, so the
    # more of them there are, the longer each one takes.
    seconds = 60 * len(ports)
    step(f"Waiting for every instance to answer (up to {seconds} s)")
    silent = common.wait_until_responds(ports, seconds, gave_up=died)
    if silent:
        fail(f"No answer on port(s) {', '.join(map(str, silent))}. See the log:\n"
             f"  sudo journalctl -u {common.instance_unit(silent[0])} -n 50 --no-pager")
    common.write_state({**state, "commit": commit})
    ok("all instances are answering")

    step("Done!")
    print(f"  Status: {common.script_command('status.py')}")
    print(f"  Logs:   {common.INSTANCES_LOG}")


def parse_args():
    parser = common.new_parser(__doc__)
    parser.add_argument("--port", required=True, type=port_range, metavar="START:END",
                        help="ports to serve on, inclusive; a single port also works (3000)")
    parser.add_argument("--restart", action="store_true",
                        help="systemd restarts an instance that dies (Restart=always)")
    parser.add_argument("--user", metavar="USER",
                        help="user the services run as (default: the user who called sudo)")
    return parser.parse_args()


def port_range(text):
    """Converts "3000:3005" (or just "3000") into the list of ports."""
    numbers = [common.port_number(part) for part in text.split(":")]
    if len(numbers) == 1:
        numbers = numbers * 2  # a single port: the range starts and ends on it
    first, last = numbers
    if first > last:
        raise ValueError(text)
    return list(range(first, last + 1))


def calling_user():
    """The user who called sudo. Without sudo (root itself), the owner of the project."""
    user = os.environ.get("SUDO_USER")
    return user if user and user != "root" else common.repo_owner().pw_name


def user_exists(user):
    try:
        pwd.getpwnam(user)
        return True
    except KeyError:
        return False


def died(port):
    """True if the instance of this port ended and systemd will not start it again."""
    state = common.unit_properties(common.instance_unit(port), "ActiveState")
    return state.get("ActiveState") == "failed"


def template_unit(user, restart):
    """The unit file shared by all instances; systemd replaces %i with the port."""
    python = common.unit_path(sys.executable)
    test = common.unit_path(common.SCRIPTS / "test.py")
    return f"""\
# Written by scripts/system-ctl-install.py. Reinstalling overwrites this file.
# One instance per port: {NAME}@3000.service is the server on port 3000.

[Unit]
Description={NAME} server on port %i
After=network.target
# With Restart=always, never give up restarting.
StartLimitIntervalSec=0

[Service]
User={user}
ExecStart={python} {test} --minport %i --maxport %i
Restart={"always" if restart else "no"}
RestartSec=5
# Wrangler ends with code 143 when systemd stops it: that is not a failure.
SuccessExitStatus=143

[Install]
WantedBy=multi-user.target
"""


if __name__ == "__main__":
    main()

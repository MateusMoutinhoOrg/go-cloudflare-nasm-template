#!/usr/bin/env python3
"""Turns the watcher on: a systemd timer that runs update.py at every interval.

The interval counts from the end of the previous run, so two runs never
overlap. A run that fails does not turn the watcher off: it tries again
after the next interval. Installing again replaces the previous watcher.

example:
  sudo python3 scripts/watcher-install.py --interval 60
"""

import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import NAME, fail, ok, step


def main():
    args = parse_args()
    common.require_systemd()
    common.require_root()
    common.lock()
    common.require_installed()

    step("Checking that git can reach the repository without a password (git fetch)")
    if not common.git("fetch"):
        fail("git fetch failed or needs a password. The watcher runs without anyone to\n"
             "type it: the repository must be public or have an access key configured.")
    ok("git fetch works")

    step(f"Installing the watcher (update.py every {args.interval} seconds)")
    common.remove_watcher()
    common.write_unit(common.WATCHER_SERVICE, service_unit())
    common.write_unit(common.WATCHER_TIMER, timer_unit(args.interval))
    common.systemctl("daemon-reload")
    common.systemctl("enable", "--quiet", "--now", common.WATCHER_TIMER.name)
    ok(f"enabled at boot and started; first run in {args.interval} seconds")

    step("Done!")
    print(f"  Status: {common.script_command('status.py')}")
    print(f"  Logs:   {common.WATCHER_LOG}")


def parse_args():
    parser = common.new_parser(__doc__)
    parser.add_argument("--interval", type=seconds, default=60, metavar="SECONDS",
                        help="seconds between the end of a run and the next one (default: 60)")
    return parser.parse_args()


def seconds(text):
    """Converts a command-line value to a number of seconds (at least 1)."""
    number = int(text)
    if number < 1:
        raise ValueError(text)
    return number


def service_unit():
    """What the watcher runs: update.py, once, as root."""
    python = common.unit_path(sys.executable)
    update = common.unit_path(common.SCRIPTS / "update.py")
    return f"""\
# Written by scripts/watcher-install.py. Reinstalling overwrites this file.

[Unit]
Description={NAME} watcher: updates the server when the repository changes

[Service]
Type=oneshot
ExecStart={python} {update}
# An update that hangs (e.g. network) is stopped, so the next one can run.
TimeoutStartSec=1800
"""


def timer_unit(interval):
    """When the watcher runs."""
    return f"""\
# Written by scripts/watcher-install.py. Reinstalling overwrites this file.

[Unit]
Description=Runs the {NAME} watcher every {interval} seconds

[Timer]
# First run: {interval} seconds after the timer starts (now and at every boot).
OnActiveSec={interval}
# Next runs: {interval} seconds after the previous one ENDS, so they never overlap.
OnUnitInactiveSec={interval}
AccuracySec=1

[Install]
WantedBy=timers.target
"""


if __name__ == "__main__":
    main()

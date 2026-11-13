#!/usr/bin/env python3
"""Shows what is installed in systemd and whether it is answering.

Exit code: 0 when everything is fine; 1 when an instance does not answer or
when the last run of the watcher failed. Does not need sudo.
"""

import re
import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import step


def main():
    common.new_parser(__doc__).parse_args()
    common.require_systemd()

    state = common.read_state()
    if state is None:
        print("Nothing is installed in systemd.")
        print("To install: sudo "
              + common.script_command("system-ctl-install.py", "--port", "3000:3005"))
        return

    instances_fine = show_instances(state)
    watcher_fine = show_watcher()
    show_commit(state)
    show_logs()
    sys.exit(0 if instances_fine and watcher_fine else 1)


def show_instances(state):
    """Prints one line per instance. Returns True if all of them answer."""
    restart = "restarted by systemd if they die" if state["restart"] else "not restarted if they die"
    step(f"Instances (running as {state['user']}, {restart})")
    all_answer = True
    for port in state["ports"]:
        active = common.unit_properties(common.instance_unit(port), "ActiveState")
        answers = common.responds(port)
        all_answer = all_answer and answers
        print(f"  {'✔' if answers else '✘'} port {port}: "
              f"{active.get('ActiveState', 'unknown')}, "
              f"{'answering' if answers else 'NOT answering'}")
    return all_answer


def show_watcher():
    """Prints the state of the watcher. Returns False if its last run failed."""
    step("Watcher")
    if not common.WATCHER_TIMER.exists():
        print("  not installed (to install: sudo "
              + common.script_command("watcher-install.py") + ")")
        return True

    timer = common.unit_properties(common.WATCHER_TIMER.name, "ActiveState")
    interval = re.search(r"OnUnitInactiveSec=(\d+)", common.WATCHER_TIMER.read_text())
    print(f"  {'active' if timer.get('ActiveState') == 'active' else 'NOT active'}, "
          f"every {interval.group(1) if interval else '?'} seconds")

    run = common.unit_properties(common.WATCHER_SERVICE.name,
                                 "ActiveState", "Result", "ExecMainStartTimestamp")
    if run.get("ActiveState") == "activating":
        print("  last run: running right now")
        return True
    if not run.get("ExecMainStartTimestamp"):
        print("  last run: none yet")
        return True
    succeeded = run.get("Result") == "success"
    print(f"  last run: {run['ExecMainStartTimestamp']} "
          f"({'✔ succeeded' if succeeded else '✘ FAILED'})")
    return succeeded


def show_commit(state):
    step("Commit")
    installed, current = state["commit"], common.current_commit()
    print(f"  installed:  {installed[:12] if installed else 'none (the last installation did not finish)'}")
    print(f"  repository: {current[:12] if current else 'unknown (git could not read it)'}")
    if current and installed != current:
        print("  they differ; to install the repository version: sudo "
              + common.script_command("update.py"))


def show_logs():
    step("Logs")
    print(f"  instances: {common.INSTANCES_LOG}")
    print(f"  watcher:   {common.WATCHER_LOG}")


if __name__ == "__main__":
    main()

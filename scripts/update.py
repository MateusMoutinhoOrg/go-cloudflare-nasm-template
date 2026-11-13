#!/usr/bin/env python3
"""Downloads what is new in the repository and, if the commit changed, reinstalls.

Steps, in this order:
  1. git pull (fast-forward only: local changes are never discarded)
  2. install.py (dependencies, and checks that the new code compiles)
  3. system-ctl-install.py with the same parameters as the last installation

"Changed" means: the commit of the repository is not the commit recorded by
the last installation that worked. So an update that failed halfway is tried
again the next time. This is the script the watcher runs.
"""

import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import fail, ok, step


def main():
    common.new_parser(__doc__).parse_args()
    common.require_systemd()
    common.require_root()
    common.lock()
    state = common.require_installed()

    step("Downloading what is new (git pull)")
    if not common.git("pull", "--ff-only"):
        fail("git pull failed. Nothing was changed.")
    commit = common.current_commit()
    if commit is None:
        fail("Could not read the commit of the repository. Nothing was changed.")
    if commit == state["commit"]:
        return ok(f"nothing changed (commit {commit[:7]}); nothing was restarted")
    ok(f"installed: {(state['commit'] or 'none')[:7]}  repository: {commit[:7]}")

    # Each step below is a new process, so it runs the scripts just downloaded.

    step("Installing dependencies and compiling (install.py)")
    # As the owner of the project, not as root, so no root files are left in it.
    if not common.run_script("install.py", **common.owner_options()):
        fail("install.py failed. systemd was not touched.")

    step("Reinstalling in systemd with the same parameters")
    ports = state["ports"]
    parameters = ["--port", f"{ports[0]}:{ports[-1]}", "--user", state["user"]]
    if state["restart"]:
        parameters.append("--restart")
    if not common.run_script("system-ctl-install.py", *parameters):
        fail("The reinstallation failed. It will be tried again on the next update.")


if __name__ == "__main__":
    main()

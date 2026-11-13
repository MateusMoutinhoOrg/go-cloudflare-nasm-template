#!/usr/bin/env python3
"""Removes the server from systemd: every instance, the watcher and the installation state.

The repository, the dependencies and the local database are not touched.
"""

import shutil
import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ok, step


def main():
    common.new_parser(__doc__).parse_args()
    common.require_systemd()
    common.require_root()
    common.lock()

    step("Removing the server from systemd")
    had_watcher = common.remove_watcher()  # first, so it cannot start an update meanwhile
    instances = common.remove_instances()
    had_files = common.TEMPLATE_UNIT.exists() or common.STATE_FILE.parent.exists()
    common.TEMPLATE_UNIT.unlink(missing_ok=True)
    shutil.rmtree(common.STATE_FILE.parent, ignore_errors=True)
    common.systemctl("daemon-reload")

    if not (had_watcher or instances or had_files):
        return ok("nothing was installed")
    if had_watcher:
        ok("watcher removed")
    ok(f"{instances} instance(s) stopped and removed")
    ok("the repository, the dependencies and the local database were not touched")


if __name__ == "__main__":
    main()

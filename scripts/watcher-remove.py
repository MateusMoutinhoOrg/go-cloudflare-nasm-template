#!/usr/bin/env python3
"""Turns the watcher off: stops, disables and deletes its timer and its service.

The instances of the server are not touched.
"""

import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ok, step


def main():
    common.new_parser(__doc__).parse_args()
    common.require_systemd()
    common.require_root()
    common.lock()

    step("Removing the watcher")
    if common.remove_watcher():
        ok("watcher removed; the instances of the server were not touched")
    else:
        ok("the watcher was not installed")


if __name__ == "__main__":
    main()

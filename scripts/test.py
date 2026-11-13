#!/usr/bin/env python3
"""Runs the server locally, on the first free port of a range.

When a file of the sandbox is saved, the server recompiles and reloads.
"""

import os
import socket
import sys

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ROOT, WRANGLER, fail, step


def main():
    args = parse_args()
    common.refuse_sudo()
    common.require_dependencies()

    port = first_free_port(args.minport, args.maxport)
    if port is None:
        fail(f"No free port between {args.minport} and {args.maxport}.")

    env = common.tools_env()
    os.chdir(ROOT)  # wrangler.jsonc lives at the root

    step(f"Starting the server at http://localhost:{port}")
    sys.stdout.flush()
    # --inspector-port 0: the system chooses the debugger port. Left alone,
    # Wrangler takes 9229 if it looks free, so servers that start at the same
    # moment (the systemd instances, at every boot) all take it, and all but
    # one die with "Address already in use".
    command = ["wrangler", "dev", "--port", str(port), "--inspector-port", "0"]
    # From here on this process becomes Wrangler (so Ctrl+C and systemd talk to it directly).
    os.execve(WRANGLER, command, env)


def parse_args():
    parser = common.new_parser(__doc__)
    parser.add_argument("--minport", type=common.port_number, default=3000, metavar="PORT",
                        help="first port to try (default: 3000)")
    parser.add_argument("--maxport", type=common.port_number, default=4000, metavar="PORT",
                        help="last port to try (default: 4000)")
    args = parser.parse_args()
    if args.minport > args.maxport:
        parser.error("--minport cannot be greater than --maxport")
    return args


def first_free_port(first, last):
    """The first port of the range with nothing listening on it, or None."""
    return next((port for port in range(first, last + 1) if is_free(port)), None)


def is_free(port):
    """True if nothing answers on this port of localhost."""
    try:
        socket.create_connection(("localhost", port), timeout=1).close()
        return False
    except OSError:  # connection refused: nobody is there
        return True


if __name__ == "__main__":
    main()

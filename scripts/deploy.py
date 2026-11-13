#!/usr/bin/env python3
"""Publishes the Worker to Cloudflare.

Authentication: the CLOUDFLARE_API_TOKEN environment variable, if set;
otherwise the session created by `wrangler login`.
"""

import json
import os
import shlex
import sys
import tempfile
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ENGINE, ROOT, WRANGLER, fail, ok, step


def main():
    args = parse_args()
    common.refuse_sudo()
    common.require_dependencies()
    env = common.tools_env()
    if not args.dry_run:
        require_cloudflare_login(env)

    step("Compiling the Go code to WebAssembly")
    if not common.run([ENGINE / "compile.sh"], env=env):
        fail("The Go code does not compile. Nothing was published.")

    if args.dry_run:
        step("Validating the Worker (dry run: nothing is published)")
        with tempfile.TemporaryDirectory() as bundle:
            command = [WRANGLER, "deploy", "--dry-run", "--outdir", bundle]
            if not common.run(command, cwd=ROOT, env=env):
                fail("The Worker is not valid.")
        return ok("the Worker is valid; run again without --dry-run to publish")

    step("Publishing the Worker to Cloudflare")
    url = publish(env)

    step(f"Done! Worker URL: {url}")


def parse_args():
    parser = common.new_parser(__doc__)
    parser.add_argument("--dry-run", action="store_true",
                        help="compile and validate, without publishing")
    return parser.parse_args()


def require_cloudflare_login(env):
    """Ends the script, before anything is published, if there is no way to authenticate."""
    step("Cloudflare authentication")
    if env.get("CLOUDFLARE_API_TOKEN"):
        return ok("using CLOUDFLARE_API_TOKEN from the environment")

    # `wrangler whoami --json` only succeeds when there is a valid login session.
    if common.output([WRANGLER, "whoami", "--json"], cwd=ROOT, env={**env, "CI": "true"}):
        return ok("using the `wrangler login` session")

    fail("Not authenticated with Cloudflare. Choose one:\n"
         "  - set the CLOUDFLARE_API_TOKEN environment variable to an API token\n"
         "    (https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)\n"
         f"  - or log in once with: . {shlex.quote(os.path.relpath(ENGINE / 'env.sh'))} && wrangler login")


def publish(env):
    """Runs `wrangler deploy` and returns the URL of the Worker."""
    with tempfile.TemporaryDirectory() as folder:
        # Wrangler writes what it did to this file, one JSON per line.
        report = Path(folder) / "deploy.ndjson"
        env = {**env, "WRANGLER_OUTPUT_FILE_PATH": str(report)}
        if not common.run([WRANGLER, "deploy"], cwd=ROOT, env=env):
            fail("The deploy failed.")
        lines = report.read_text().splitlines() if report.exists() else []
        events = [json.loads(line) for line in lines if line.strip()]

    for event in events:
        if event.get("type") == "deploy":
            urls = [target for target in event.get("targets") or [] if target.startswith("https://")]
            if urls:
                return urls[0]
    return "(not reported by Wrangler; see the output above)"


if __name__ == "__main__":
    main()

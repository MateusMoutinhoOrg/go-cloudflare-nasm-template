#!/usr/bin/env python3
"""Installs everything the project needs: Go, Node.js/npm and Wrangler.

Only what is missing is installed, without sudo and without touching the
system: Go and Node.js are downloaded into engine/tools/, Wrangler into
engine/node_modules/. At the end, the Go code is compiled once as a test.
"""

import hashlib
import json
import platform
import re
import shutil
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ left in the project
import common
from common import ENGINE, TOOLS, WRANGLER, fail, ok, step

NODE_LINE = "24"  # LTS line downloaded when there is no suitable Node.js


def main():
    common.new_parser(__doc__).parse_args()
    common.refuse_sudo()

    install_go()
    install_node()
    install_wrangler()
    test_build()

    step(f"Done! To run locally: {common.script_command('test.py')}")


def install_go():
    step(f"Go (>= {common.GO_MIN})")
    version = common.go_version()
    if common.version_at_least(version, common.GO_MIN):
        return ok(f"using Go {version} at {common.find_tool('go')}")

    system, go_arch, _ = platform_names()
    release = fetch_text("https://go.dev/VERSION?m=text").split()[0]  # e.g. "go1.27.3"
    url = f"https://dl.google.com/go/{release}.{system}-{go_arch}.tar.gz"
    install_archive(url, sha256=fetch_text(url + ".sha256").strip(), target=TOOLS / "go")

    if not common.version_at_least(common.go_version(), common.GO_MIN):
        fail("Go was downloaded but does not run on this machine.")
    ok(f"installed Go {common.go_version()} in engine/tools/go")


def install_node():
    step(f"Node.js and npm (>= {common.NODE_MIN})")
    version = common.node_version()
    if common.version_at_least(version, common.NODE_MIN):
        return ok(f"using Node.js {version} at {common.find_tool('node')}")

    system, _, node_arch = platform_names()
    base = f"https://nodejs.org/dist/latest-v{NODE_LINE}.x"
    # Each line of SHASUMS256.txt is "<sha256>  <file name>".
    checksums = {}
    for line in fetch_text(f"{base}/SHASUMS256.txt").splitlines():
        sha256, name = line.split()
        checksums[name] = sha256
    wanted = re.compile(rf"node-v[0-9.]+-{system}-{node_arch}\.tar\.gz")
    archive = next((name for name in checksums if wanted.fullmatch(name)), None)
    if archive is None:
        fail(f"Could not find Node.js {NODE_LINE} for {system}-{node_arch}.")
    install_archive(f"{base}/{archive}", sha256=checksums[archive], target=TOOLS / "node")

    if not common.version_at_least(common.node_version(), common.NODE_MIN):
        fail("Node.js was downloaded but does not run on this machine.")
    ok(f"installed Node.js {common.node_version()} in engine/tools/node")


def install_wrangler():
    step("Wrangler")
    lock_file = ENGINE / "package-lock.json"
    if lock_file.exists():
        wanted = json.loads(lock_file.read_text())["packages"]["node_modules/wrangler"]["version"]
        if wrangler_version() == wanted:
            return ok(f"using Wrangler {wanted} in engine/node_modules")
        npm = ["ci"]  # installs exactly what package-lock.json says
    else:
        npm = ["install", "--save-dev", "--save-exact", "wrangler@4"]

    command = [common.find_tool("npm"), *npm, "--no-audit", "--no-fund"]
    if not common.run(command, cwd=ENGINE, env=common.tools_env()):
        fail("npm could not install Wrangler.")
    if wrangler_version() is None:
        fail("Wrangler was installed but does not run.")
    ok(f"installed Wrangler {wrangler_version()} in engine/node_modules")


def test_build():
    step("Compiling the Go code to WebAssembly (test)")
    if not common.run([ENGINE / "compile.sh"], env=common.tools_env()):
        fail("The Go code does not compile.")
    size = (ENGINE / "build" / "server.wasm").stat().st_size
    ok(f"engine/build/server.wasm ({size / 1_000_000:.1f} MB)")


def wrangler_version():
    """Version of the Wrangler in engine/node_modules, or None if it is not there."""
    return common.tool_version(WRANGLER, "--version")


def platform_names():
    """How the Go and Node.js download files call this machine: (system, go_arch, node_arch)."""
    systems = {"Darwin": "darwin", "Linux": "linux"}
    machines = {"x86_64": ("amd64", "x64"), "amd64": ("amd64", "x64"),
                "arm64": ("arm64", "arm64"), "aarch64": ("arm64", "arm64")}
    if platform.system() not in systems:
        fail(f"Unsupported operating system: {platform.system()}")
    if platform.machine().lower() not in machines:
        fail(f"Unsupported architecture: {platform.machine()}")
    return (systems[platform.system()], *machines[platform.machine().lower()])


def install_archive(url, sha256, target):
    """Downloads a .tar.gz, checks it and installs its top-level folder as `target`."""
    print(f"  downloading {url} ...", flush=True)
    TOOLS.mkdir(parents=True, exist_ok=True)
    # Everything is prepared in a temporary folder next to the target, so an
    # interrupted download never leaves a half-installed tool behind.
    with tempfile.TemporaryDirectory(dir=TOOLS) as temporary:
        archive = Path(temporary) / "archive.tar.gz"
        download(url, archive)
        if hashlib.sha256(archive.read_bytes()).hexdigest() != sha256:
            fail(f"Invalid checksum for {url}")

        content = Path(temporary) / "content"
        with tarfile.open(archive) as tar:
            if hasattr(tarfile, "data_filter"):  # refuses files that escape the folder
                tar.extractall(content, filter="data")
            else:  # older Python
                tar.extractall(content)
        folders = list(content.iterdir())  # ["go"] or ["node-v24.1.0-linux-x64"]
        if len(folders) != 1:
            fail(f"Unexpected content in {url}")
        shutil.rmtree(target, ignore_errors=True)
        folders[0].rename(target)


def download(url, file):
    """Downloads a URL into a file.

    curl is used when it exists, because it knows the system's certificates
    on every operating system; otherwise Python itself downloads.
    """
    if shutil.which("curl"):
        if not common.run(["curl", "-fsSL", "--retry", "3", url, "-o", file]):
            fail(f"Could not download {url}")
        return
    try:
        with urllib.request.urlopen(url, timeout=60) as response, open(file, "wb") as target:
            shutil.copyfileobj(response, target)
    except OSError as error:
        fail(f"Could not download {url}: {error}")


def fetch_text(url):
    """Downloads a small text file and returns its content."""
    with tempfile.NamedTemporaryFile() as file:
        download(url, file.name)
        return Path(file.name).read_text()


if __name__ == "__main__":
    main()

# go-cloudflare-nasm-template

**Go web server template — Cloudflare Worker + SQLite**

> [!WARNING]
> **This project is an experiment and will never have a stable version.**
> Anything in it may change or break without notice. If you build on it, treat
> your fork as your own code.

go-cloudflare-nasm-template is a template for writing a web server in Go. Your
code is compiled to WebAssembly and runs in a Cloudflare Worker, with SQLite
databases.

Fork it, rename it and write your server in `sandbox/`.

- [Getting started](#getting-started)
- [Writing your server](#writing-your-server)
- [Building and testing](#building-and-testing)
- [Scripts](#scripts)
- [Deploying to Cloudflare](#deploying-to-cloudflare)
- [Running on your own machine (systemd)](#running-on-your-own-machine-systemd)
- [Where everything is](#where-everything-is)
- [License](#license)

## Getting started

You need `git` and Python 3.9 or newer, on Linux or macOS.

1. **Fork** [this repository](https://github.com/MateusMoutinhoOrg/go-cloudflare-nasm-template)
   (the *Fork* button on GitHub) and clone your fork:

   ```sh
   git clone https://github.com/YOUR-USER/YOUR-REPOSITORY.git
   cd YOUR-REPOSITORY
   ```

2. **Rename** the project to your repository:

   ```sh
   python3 scripts/rename.py
   ```

   It reads the address of your fork from git and changes the Go module, the
   name of the Worker and the name of the systemd services. To choose the name
   yourself: `python3 scripts/rename.py github.com/YOUR-USER/YOUR-REPOSITORY`.

3. **Install** Go, Node.js and Wrangler. Only what is missing is installed,
   inside the project and without sudo:

   ```sh
   python3 scripts/install.py
   ```

4. **Run** the server and open the address it prints
   (<http://localhost:3000>, if that port is free). You will see the example
   app, Linkbox:

   ```sh
   python3 scripts/test.py
   ```

5. **Save** the rename in your fork:

   ```sh
   git commit -am "Rename the project"
   git push
   ```

## Writing your server

Your server is the function `SandboxMain`, in `sandbox/sandboxmain.go`. It runs
once for every request:

```go
func SandboxMain(deps deps.Deps) {
	server := deps.Server

	switch {
	case server.Match("/hello/:name"):
		server.Text(200, "Hello, "+server.Param("name")+"!")
	default:
		server.Text(404, "Route not found.")
	}
}
```

Everything it can use arrives in `deps`. The sandbox that comes with the
template is a small app, **Linkbox**: you paste the address of a page, and the
server visits it, reads its title and saves the link. It uses all three:

| In `deps`          | What it is for                          | In Linkbox                        | Declared in          |
| ------------------ | --------------------------------------- | --------------------------------- | -------------------- |
| `deps.Server`      | read the request and write the response | answers the page and `/api/links` | `deps/server.go`     |
| `deps.HTTPClient`  | make HTTP requests to other servers     | visits a page to read its title   | `deps/httpclient.go` |
| `deps.DatabaseHub` | SQLite databases, created on first use  | keeps the links                   | `deps/database.go`   |

The route `/` is the frontend, `sandbox/index.html`. Its buttons call
`/api/links`, which you can also try from a terminal:

```sh
curl localhost:3000/api/links                           # lists the links
curl localhost:3000/api/links -d '{"url": "go.dev"}'    # saves a link
curl -X DELETE localhost:3000/api/links/1               # removes a link
```

Read the example, then replace it with your own server. You may add as many
files to `sandbox/` as you want.

Linkbox has no login: whoever can open it can add and remove links, and make
the server visit any address. Replace it, or protect it, before you leave a
server open to the internet.

Limits you should know about:

- The creation query of a database runs only once, when the database is
  created. If you change it later, the databases that already exist keep their
  tables: connect with a new id (`"links-v2"`) or, on your machine, stop the
  server and delete `.wrangler/state` to start again from zero.
- Every SQL number arrives as a `float64`. An integer beyond ±2^53 is refused
  as a query argument, because it would lose digits: pass it as a string.
- `RemoteAddr` is only the IP of the client, without the port.
- A header sent more than once arrives as a single value (`"a, b"`).
- `Context` is not canceled when the client disconnects under `test.py`, only
  on Cloudflare.

## Building and testing

There is nothing to build by hand. `test.py` compiles the Go code when it
starts and again every time you save a file, and `deploy.py` compiles before
publishing.

To only compile, for example to check for errors, or to run the tests:

```sh
engine/compile.sh   # writes engine/build/server.wasm
engine/test.sh      # runs go vet and the tests
```

The tests are the files named `*_test.go`. The ones that come with the
template check `engine/`; the ones you add to `sandbox/` run with them.

## Scripts

They are in `scripts/`, need only Python, work from any directory and explain
themselves with `--help`.

| Script                  | What it does                                               | sudo |
| ----------------------- | ---------------------------------------------------------- | ---- |
| `rename.py`             | Renames the project (run it once, after you fork)          | no   |
| `install.py`            | Installs Go, Node.js and Wrangler inside the project       | no   |
| `test.py`               | Runs the server locally, on the first free port from 3000  | no   |
| `deploy.py`             | Publishes the server to Cloudflare                         | no   |
| `system-ctl-install.py` | Registers the server in systemd, one instance per port     | yes  |
| `system-ctl-remove.py`  | Removes everything from systemd                            | yes  |
| `status.py`             | Shows what is installed in systemd and if it is answering  | no   |
| `update.py`             | Runs `git pull` and, if something changed, reinstalls      | yes  |
| `watcher-install.py`    | Turns the watcher on: runs `update.py` at every interval   | yes  |
| `watcher-remove.py`     | Turns the watcher off                                      | yes  |

The last six need Linux with systemd. Never use sudo with the first four: it
would leave files owned by root in the project.

## Deploying to Cloudflare

```sh
python3 scripts/deploy.py             # compiles and publishes; prints the URL
python3 scripts/deploy.py --dry-run   # only compiles and validates
```

Before the first deploy, log in to Cloudflare (or set the
`CLOUDFLARE_API_TOKEN` environment variable instead):

```sh
. engine/env.sh && wrangler login
```

There is no database to create: each one is created the first time your
server connects to it.

What your server prints, and the reason of every request that fails, goes to
the log of the Worker: in the Cloudflare dashboard (your Worker, then *Logs*),
or live with `. engine/env.sh && wrangler tail`. Locally, the log is the
terminal of `test.py`.

Cloudflare's Git build (Workers Builds) also works with its default settings.
The name of the Worker in the dashboard must be the `name` in `wrangler.jsonc`.

## Running on your own machine (systemd)

To keep the server running on a Linux machine, and up to date with your
repository:

```sh
python3 scripts/install.py
sudo python3 scripts/system-ctl-install.py --port 3000:3005 --restart
sudo python3 scripts/watcher-install.py --interval 60
python3 scripts/status.py
```

- `system-ctl-install.py` starts one instance per port, now and at every boot.
  With `--restart`, systemd restarts an instance that dies.
- `watcher-install.py` runs `update.py` every 60 seconds. The repository must
  be reachable without a password (public, or with an access key configured).
- The instances listen on `localhost` only and share one local database
  (`.wrangler/state`).
- `sudo python3 scripts/system-ctl-remove.py` undoes all of it; the repository
  and the database are kept.

## Where everything is

```
sandbox/          YOUR SERVER (start here)
deps/             what the sandbox can use (declarations only)
engine/           what makes it run: the Cloudflare Worker and the build
scripts/          rename, install, run, deploy and manage the server
wrangler.jsonc    configuration of the Worker
go.mod            name of the Go module
LICENSE           the Unlicense
```

For each request, the Worker (`engine/js/worker.mjs`) starts the Go program
(`engine/worker`), which builds the `deps` and calls `SandboxMain`. The sandbox
imports nothing from `engine/`: it only knows `deps`.

## License

This project is in the public domain, under the [Unlicense](LICENSE): you may
copy, change, publish and sell it, for any purpose, without asking and without
giving credit. It comes with no warranty.

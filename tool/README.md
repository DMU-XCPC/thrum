# The workspace tool

`ws` is the tooling `README.md` designs and `TOOLING.md` specifies. It is pure Go with
cgo disabled, and it shells out to `git` rather than reimplementing it.

It adds no rules of its own. Every command below is an operation from `TOOLING.md`, and
every rule it enforces is one from `README.md`; where this file and those disagree, they
are right and this file is wrong.

## Build, test, try

```sh
make build      # writes .gocache/ws
make test       # every package, plus the end-to-end fixture
make vet fmt tidy
```

Nothing has to be installed to try it: the binary lands in `.gocache/`, beside the
module and build caches. If the module proxy is unreachable, the Makefile already uses
`goproxy.cn`; `make` is the only supported way to build for that reason.

```sh
./.gocache/ws create /tmp/demo --site build-01
cd /tmp/demo && .../ws verify
```

## The command surface, and the operation it implements

| `TOOLING.md` | Here |
| --- | --- |
| `create` | `ws create <dir> [--site]` |
| `verify` | `ws verify` |
| `status` | `ws status` |
| `clean` | `ws clean [--dry-run]` |
| `sync` | `ws sync [--job <id>] [--fetch]` |
| `registry` | `ws actor registry` |
| `ask` | `ws actor ask --job <id>` |
| `start` | `ws daemon start --job <id> -- <command>…` |
| `wake` | `ws daemon wake --job <id>` (also `notify`) |
| `open` | `ws open --job <id> --label <label>` |
| `claim` | `ws claim --job <id> [--owner <actor>]` |
| `reassign`, `relinquish` | `ws claim --owner <actor>`, `ws relinquish` |
| `close` | `ws close --job <id> [--abandoned]` |
| `add worktree`, `release worktree` | `ws worktree add`, `ws worktree release` |
| `occupy`, `vacate`, `sweep` | `ws occupancy occupy`, `vacate`, `sweep` |
| `declare`, `remove`, `resources` | `ws resource declare`, `remove`, `list` |
| `take lease`, `renew`, `release`, `leases` | `ws lease take`, `renew`, `release`, `list` |
| `ingest`, `publish`, `materialise`, `withdraw`, `store` | `ws asset ingest`, `publish`, `materialise`, `withdraw`, `store` |
| `write handoff` | `ws handoff --job <id> --status <s>` |
| `commit` | not a command: every operation commits its own one event |
| `collect` | `ws collect [--window] [--dry-run]` |
| `export`, `import` | `ws bundle export`, `ws bundle import <bundle>` |
| `coordinate` | `ws daemon coordinate --resource <name> --job <id>` |
| `serve` | `ws daemon run --ssh-listen <where>` (also `serve`) |
| `peers` | `ws daemon peers` |

`--root` is a global flag, and defaults to where the command was run. `--actor` is
global too and defaults to `$WS_ACTOR`, then `$USER`: an actor is a stable identity and
not a per-command choice.

## With a daemon, and without one

Every command works with no daemon running, because each does its own work and returns.
The daemon exists for the three things a file cannot do:

```sh
ws daemon run                              # local socket only
ws daemon run --ssh-listen 0.0.0.0:2222    # and answer peers
```

- **Being alive.** `ws daemon start` runs an actor and supervises it. With no daemon it
  refuses and says so, rather than starting something nothing is watching.
- **Waking something.** Writing `blocked` in a handoff records that a job is waiting;
  only a process can act on it when the wait ends.
- **Across machines.** A lease is taken at the site the resource lives on. The site
  holding it is found by asking, and a peer that cannot be reached is reported rather
  than assumed.

One daemon per site, enforced by `var/daemon.lock`, which names the process holding it.
A lock left behind by a process that is gone is taken over by whoever runs next.

## Reaching a peer

How one site reaches another is the site's own business and lives in
`var/config/<name>.yaml`, never in git:

```yaml
reach: build-01.internal:22     # or user@host:port, or unix:/run/ws.sock
ssh_user: root                   # optional
key: /etc/ws/id_ed25519          # optional; var/secrets/id_ed25519 is the default
```

Authentication is SSH's and nothing else: the daemon accepts a peer whose public key is
in `var/secrets/authorized_keys`, and refuses every other key. A peer may ask for
exactly two things — the forced command, and this workspace's own repositories, so that
the records and branches can travel by git. A path outside the workspace is refused, and
there is no shell.

The daemon can serve SSH itself, or a system `sshd` can be pointed at it:

```sshconfig
# authorized_keys on the serving side
command="ws daemon exec",no-port-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA…
```

Either way the protocol is the same one request, one response, and both are the same
code path.

## What it refuses, on purpose

- A job id that has been used before, or a label that is not declared, or a label that
  already holds another job.
- A second actor on a job somebody is already working.
- Overwriting occupancy that names another actor; taking over is explicit.
- Removing a resource whose directory exists.
- A lease for a job the workspace does not know, for an occupied unit, or for renewal by
  anybody but its holder.
- Publishing a version twice, reusing a withdrawn version number, or moving a tag.
- Replacing a payload without being asked, and importing one whose records have not
  arrived.
- Writing a handoff over an existing handoff.
- `git clean` with `-x`, or with `-d`: the first would delete `.bare/` and the source
  history with it, the second would demolish the workspace's own empty directories.
- Repairing anything in `verify`. It reports; a wrong repair costs more than a stale
  line.

## Where it writes

Under `--root`, and nowhere else: `doc/`, `handoff/` and `store/` are the versioned
half, `var/` is this site's runtime, and `job/<job-id>/` is a job's payload. An actor
started by the daemon gets `WS_ROOT`, `WS_JOB`, `TMPDIR`, `XDG_CACHE_HOME` and
`XDG_STATE_HOME` pointed into those, so that what it leaves behind is collected and
reviewed rather than lost.

`var/secrets/` holds machine-local credentials, including the daemon's own host key and
its `authorized_keys`. Prefer keeping it outside the workspace; it is gitignored either
way, and it never belongs in a handoff or a bundle.

## Tests

Each package tests its own rules. `internal/validate` has the checks, `internal/format`
the formats, and `e2e` drives the built command through a whole job's life — create,
declare, open, occupy, lease, hand off, publish, materialise, export, close, collect —
and asserts after every step that the workspace still passes `ws verify`. The daemon's
tests run two sites against each other over a real SSH channel.

# The workspace tool

`thrum` is the tooling `README.md` designs and `TOOLING.md` specifies. It is pure Go with
cgo disabled, and it shells out to `git` rather than reimplementing it.

It adds no rules of its own. Every command below is an operation from `TOOLING.md`, and
every rule it enforces is one from `README.md`; where this file and those disagree, they
are right and this file is wrong.

## Build, test, try

```sh
make build      # writes .gocache/thrum
make test       # every package, plus the end-to-end fixture
make vet fmt tidy
```

Nothing has to be installed to try it: the binary lands in `.gocache/`, beside the
module and build caches. If the module proxy is unreachable, the Makefile already uses
`goproxy.cn`; `make` is the only supported way to build for that reason.

```sh
./.gocache/thrum create /tmp/demo --site build-01
cd /tmp/demo && .../thrum verify
```

## The command surface, and the operation it implements

| `TOOLING.md` | Here |
| --- | --- |
| `create` | `thrum create <dir> [--site]` |
| `verify` | `thrum verify` |
| `status` | `thrum status` |
| `clean` | `thrum clean [--dry-run]` |
| `sync` | `thrum sync [--job <id>] [--fetch]` |
| `registry` | `thrum actor registry`, and the `running` section of `thrum status` |
| `ask` | `thrum actor ask --job <id>` |
| `start` | `thrum start --job <id> -- <command>…` |
| `wake` | not a command: closing a job, or handing it over as `done`, tells whoever is waiting. Writing a handoff as `blocked` is what starts something that can act on it |
| `open` | `thrum open --job <id> --label <label>` |
| `claim` | `thrum claim --job <id> [--owner <actor>]` |
| `reassign`, `relinquish` | `thrum claim --owner <actor>`, `thrum relinquish` |
| `close` | `thrum close --job <id> [--abandoned]` |
| `add worktree`, `release worktree` | `thrum worktree add`, `thrum worktree release` |
| `occupy`, `vacate`, `sweep` | `thrum occupancy occupy`, `vacate`, `sweep` |
| `declare`, `remove`, `resources` | `thrum resource declare`, `remove`, `list` |
| `take lease`, `renew`, `release`, `leases` | `thrum lease take`, `renew`, `release`, `list` |
| `ingest`, `publish`, `materialise`, `withdraw`, `store` | `thrum asset ingest`, `publish`, `materialise`, `withdraw`, `store` |
| `write handoff` | `thrum handoff --job <id> --status <s>` |
| `commit` | not a command: every operation commits its own one event |
| `collect` | `thrum collect [--window] [--dry-run]` |
| `export`, `import` | `thrum bundle export`, `thrum bundle import <bundle>` |
| `coordinate` | part of `thrum lease take`: a resource is leased where it lives, so the request goes to the site that holds it |
| `serve` | the daemon serves this site's repositories to its peers, for the same reason it answers the forced command |
| `peers` | the `sites` section of `thrum status` |

`--root` is a global flag, and defaults to where the command was run. `--actor` is
global too and defaults to `$THRUM_ACTOR`, then `$USER`: an actor is a stable identity and
not a per-command choice.

## The daemon is not a command

Nothing a person types names it. Every operation works without one, because each does its
own work and returns; the daemon exists for the two things a file cannot do — supervising
an actor, and acting later on behalf of a job that is waiting.

So `thrum` starts one when an operation needs something alive, and the daemon leaves on
its own once nothing is running and nothing is waiting:

- `thrum start` needs something to supervise it, and starts a daemon if there is none.
- Writing a handoff as `blocked` needs something that can wake the job when the wait
  ends, so that starts one too.
- Closing a job, or handing it over as `done`, tells the daemon here and every peer
  directly over SSH — no daemon required on the side that is telling.
- When no actor is alive and no live job is waiting, the daemon exits. There is nothing
  left for it to do, and a daemon nobody asked to run is worse than none.

Two commands are hidden, because they are for the tool itself, for an operator who wants
it under systemd, and for a system `sshd`:

```sh
thrum daemon run     # foreground; also what `thrum` runs detached for itself
thrum daemon exec    # the forced command a peer runs
```

One daemon per site, enforced by `var/daemon.lock`, which names the process holding it. A
lock left behind by a process that is gone is taken over by whoever runs next. What it
says goes to `var/daemon.log`, which is the only place to find out why it refused
something, and where it accepts peers comes from `var/daemon.yaml`:

```yaml
# var/daemon.yaml — never committed: how to reach this machine is its own business.
listen: unix:/run/thrum.sock     # or 0.0.0.0:2222; absent means it accepts nobody
```

`thrum status` prints what only the daemon and the network know: what this site is
running, and which sites answer right now.

## Reaching a peer

How one site reaches another is the site's own business and lives in
`var/config/<name>.yaml`, never in git:

```yaml
reach: build-01.internal:22     # or user@host:port, or unix:/run/thrum.sock
ssh_user: root                   # optional
key: /etc/thrum/id_ed25519          # optional; var/secrets/id_ed25519 is the default
```

Authentication is SSH's and nothing else: the daemon accepts a peer whose public key is
in `var/secrets/authorized_keys`, and refuses every other key. A peer may ask for
exactly two things — the forced command, and this workspace's own repositories, so that
the records and branches can travel by git. A path outside the workspace is refused, and
there is no shell.

The daemon can serve SSH itself, or a system `sshd` can be pointed at it:

```sshconfig
# authorized_keys on the serving side
command="thrum daemon exec",no-port-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA…
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
and asserts after every step that the workspace still passes `thrum verify`. The daemon's
tests run two sites against each other over a real SSH channel.

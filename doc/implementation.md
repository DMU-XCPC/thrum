# How the tooling is built

The design is [`design.md`](design.md) and the interface is [`tooling.md`](tooling.md). This
is the other half: what the implementation is made of, and how the parts that are not obvious
from either document work. Nothing here is a rule — where this and the design disagree, the
design is right.

## Stack

An implementation decision, not a design rule: nothing in `design.md` depends on any of it, and a workspace has to stay usable if the tool were rewritten in something else tomorrow.

**Pure Go, with cgo disabled.** One statically linked binary per platform, built with `CGO_ENABLED=0`, so it can be dropped onto a machine that has nothing but `git` and work. Cross-compilation is then free, which matters because the tool is installed rather than copied and there is no package manager to lean on.

The parts:

| Concern | Choice |
| --- | --- |
| Command surface | `cobra`, one command per operation, with completion |
| Structured files | `gopkg.in/yaml.v3`, used **only** on a front-matter block |
| Git | the real `git`, always as a subprocess, with its version checked by `verify`: the usage here is unusual enough that git has to stay the authority |
| Peer channel | `golang.org/x/crypto/ssh`, client and server, authenticated from `authorized_keys`; the serving side exposes a forced command with a small verb set rather than a shell |
| Local IPC | JSON over a unix socket at `var/daemon.sock`, with `var/daemon.lock` held by the one daemon |
| Atomicity | create-with-`O_EXCL` for locks, and write-temporary-then-`rename` for every replacement |
| Static assets | `go:embed` for `workspace-template/` |
| Tests | `go test`, golden files for every format, and one test that runs the whole validation list against a fixture workspace |
| Packaging | `goreleaser`, plus a systemd unit for the daemon |

**Front matter is edited textually.** The body of a document is copied through byte for byte and only the delimited front-matter block is decoded and re-encoded. That is why a YAML library without perfect comment round-tripping is good enough here: what must survive is the prose, and the prose never passes through the parser.

**This choice closes one open question and one worry.** Embedding the template is what makes an update derivable: the current template is in the binary, so what a workspace is missing is found by comparing the two rather than remembered, and `update` is what acts on the comparison. And because peers speak SSH, an embedded git server is not a thing that has to be built at all: the repositories are files at declared paths, and serving them is permitting a peer to read them.

### Lifecycle

- **One per site.** A second refuses to start, by the same atomic discipline the rest of this design uses for occupancy: create a lock under `var/`, and lose if it is already there.
- **Restarting is safe and loses nothing that matters.** The daemon holds almost no state of its own, because the workspace is the state: it re-reads the roster and the records when it starts, and the leases it honours are files. What a restart does lose is a message that was in flight, and nothing else: the roster and the mailboxes are files under `var/`, so they are still there when it returns. Nothing pretends otherwise.
- **Starting needs no network.** It reads the roster from the workspace, begins listening, and reports ready; peers are found later, or never.

### State

| Where | What |
| --- | --- |
| In memory | the names its site has seen and when each was last heard from, which job each says it is on, the last site roster it read, which peers it can currently reach |
| On disk, under `var/` | the actor roster; the mailboxes, with the bytes they carry; `occupy/`, which it writes and clears on an actor's behalf; its lock; its socket |
| Nowhere | job records; payloads except while carrying them; any decision about who should take which job |

## Tests

Each package tests its own rules. `internal/validate` has the checks, `internal/format`
the formats, and `e2e` drives the built command through a whole job's life — create,
declare, open, occupy, lease, hand off, publish, materialise, export, close, collect —
and through actors reaching each other, asserting after every step that the workspace
still passes `thrum verify`. The daemon's tests run two sites against each other over a
real SSH channel, including a message with an attachment.

# The implementation

`thrum` is one static Go binary. It adds no rules of its own: every command is an operation
from [`../doc/tooling.md`](../doc/tooling.md), and every rule it enforces is one from
[`../doc/design.md`](../doc/design.md). Where this directory and those disagree, they are
right and this directory is wrong.

## Build, test, try

```sh
make build      # writes .gocache/thrum, and CGO_ENABLED=0 is not optional: one static binary
make install    # PREFIX=/usr/local by default
make test       # every package, plus the end-to-end fixtures
make vet fmt tidy
```

Nothing has to be installed to try it: the binary lands in `.gocache/`, beside the module and
build caches the `Makefile` pins.

## Layout

| Path | What it is |
| --- | --- |
| `cmd/thrum/` | The commands: argument parsing, output, and the translation of a refusal into a sentence |
| `internal/workspace/` | The layout of a workspace, and creating or joining one |
| `internal/format/` | The file formats, names and values the design fixes |
| `internal/validate/` | Every mechanical check, in one place |
| `internal/commit/` | The record line: one event per commit, and what makes it linear |
| `internal/jobs/`, `internal/assets/`, `internal/leases/`, `internal/resources/`, `internal/occupancy/`, `internal/payload/`, `internal/bundles/` | One operation each |
| `internal/daemon/` | The service a site answers peers with, and how it is reached |
| `internal/gitx/`, `internal/atomic/` | Running git, and the retry discipline for one file |

Why any of it is shaped the way it is — the stack, the record line, the lock, the daemon's
process — is in [`../doc/implementation.md`](../doc/implementation.md).

# End-to-end tests

`go test ./e2e` builds `./cmd/thrum` and drives the binary: every command, the
refusals that matter, and two sites.

```sh
make test                 # the Go packages, then the harness
cd tool && go test ./e2e # this suite alone
cd tool && go test -short ./e2e   # skip the Docker test
```

The suite creates each workspace under `t.TempDir()`, so it touches nothing of
yours. Nothing here is scheduled in CI: it is a workstation suite, and the Docker
test is skipped when Docker or the image is absent.

## The Docker test

`TestDockerMultiSite` runs two sites as separate `alpine/git` containers on one
network, with a third container serving the histories. It is skipped when Docker
is not installed or `alpine/git:latest` is not present.

What it exercises over the network:

- a smart-HTTP git hub in a container, with the line, ledger, store and code
  published to it;
- `site invite` on one site and `join` on the other, including the key exchange;
- `sync` on both sides, including fetching the code repository;
- each site's daemon on its own port, the ed25519 handshake between them, and a
  message sent from an actor at one site to an actor at the other;
- a job, its handoff, the code, and a bundle carried between the containers;
- the same repository name claimed on both sites, settled deterministically;
- `verify` on both sites at the end.

`e2e/hub` is a minimal git smart-HTTP server used only by that test: the
`alpine/git` image has no `git-daemon`, and the test needs a transport, not an
authentication scheme.

The partial graph — A—B—C with no A—C edge — is covered by
`TestPartialConnectivity` (histories and messages through the relay, and the relay
going away), `TestForwardLoopAndHopLimit` (the loop and hop refusals) and
`TestDiscoveryJoinsAndFinds` (a join through the LAN query, the announced
addresses, the local view beating a fresh hint, and the stale hint expiring).
`TestSiteAnnounce` covers a committed address change, its line event, the peer
learning it on sync, and the local view still winning.

The daemon transport — `history.exchange`, `git.serve`, `thrum git-proxy` — is
covered by `TestDaemonServesGit`, `TestSyncThroughTheDaemon` and the Docker
test, which joins through the inviting daemon and stops the code hub after the
join; no repository has a thrum remote.

The multi-site plan — partitions, crashes, clock skew, revocation, three sites,
scale — is in [multisite-plan.md](multisite-plan.md). It marks what is covered,
what is partial, and which gaps it expects to expose.

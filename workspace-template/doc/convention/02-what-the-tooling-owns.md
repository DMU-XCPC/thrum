# Some files are not yours to write

The tooling writes some files, and only the tooling writes them. Editing one by hand
breaks an invariant the file exists to hold, and no check can put it back.

| Do not edit | Because |
| --- | --- |
| `doc/resource/*.yaml` | a name that has to stay unique, and a registry two sites would otherwise both append to |
| `doc/asset/**` | a hash that has to keep matching the bytes it names |
| everything under `var/` | runtime state: leases with an expiry, occupancy, this site's own name, site-local configuration |
| `store/` | a tag that must never move |

Reach for the operation instead: `thrum resource declare`, `thrum asset publish`,
`thrum lease take`, `thrum occupancy occupy`, `thrum worktree add`.

**Everything else under `doc/` and `handoff/` is yours**, written in an editor and then
recorded with the tooling:

```sh
thrum commit --summary "<what changed>" -- <paths>
```

**Never edit a handoff, not even your own.** Handoffs are append-only evidence, one per
stretch of work; a correction is a new handoff that says so.

# What survives a job, and for how long

A job's payload is transient. `job/<job-id>/tmp/` and `cache/` go as soon as the job
closes, and the expensive half — `log/`, `data/`, `state/`, `out/` — is kept for a while
and then collected.

```values
collect.window: 14d
```

That value is policy rather than a machine fact: everyone in the workspace should see the
same one, and a change to it should be auditable, so it lives here with the rule that uses
it. `thrum collect --dry-run` says what would go, and `--window` overrides it once.

Collection never makes a record false. Every artifact a handoff names keeps its raw
`sha256`, so a collected artifact is a detectable absence rather than a silent one — and a
brief with `keep: true` is never collected at all.

**A result that has to survive must be published**: `thrum asset publish` puts it in the
store and anchors it with a manifest under `doc/asset/`. `out/` is a place to work, not a
way to deliver.

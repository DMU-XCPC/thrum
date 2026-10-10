# Hand over what you learned

Stop by recording a handoff, not by stopping.

```sh
thrum handoff --job <job-id> --status <done|partial|blocked|aborted> --next "<what to do first>"
```

Say what changed, what you verified and how, what remains, and what the next actor should
do first. Name what the stretch produced with `--artifact <path>`: the tooling records its
raw hash, so an artifact that is later collected is a detectable absence rather than a
silent one.

- **`done` means the acceptance criteria hold**, not that you ran out of ideas. The job
  still has to be closed, and closing it is the owner's decision:
  `thrum close --job <job-id>`.
- **`blocked` names the jobs you are waiting on**, with `--blocked-by`. Whoever closes
  those jobs will be told to tell you.
- **Give back what you hold.** A lease lasts until it expires:
  `thrum lease release --resource <name> --unit <n>`. Occupancy is cleared with
  `thrum occupancy vacate --job <job-id>`, or by closing the job.
- **A result that has to survive is published**, not left in `job/<job-id>/out/`:
  `thrum asset publish`. What is not published is collected.

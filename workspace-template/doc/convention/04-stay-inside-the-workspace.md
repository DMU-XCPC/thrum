# Keep everything inside the workspace

A job that writes outside the workspace leaves nothing another actor can find.

- **Point tools inward first**: `eval "$(thrum env --job <job-id>)"` sets `TMPDIR`,
  `XDG_CACHE_HOME` and `XDG_STATE_HOME`. A tool with a cache variable of its own —
  `PIP_CACHE_DIR`, `GOCACHE`, `CARGO_TARGET_DIR`, `npm_config_cache` — is pointed at a
  path under the workspace when you invoke it.
- **Nothing under `worktree/` holds anything but source**, or a document that changes
  with the source. A working directory is handed from one job to the next, so anything
  else left there becomes the next actor's confusion.
- **A secret never goes into `doc/`, `handoff/`, or any git repository.** They are
  permanent, and a secret in a permanent place is a secret that has to be rotated.

---
job: <job-id>
agent: <actor>                   # the actor that wrote this
branch: job/<job-id>
base: <commit id>
head: <commit id>                # equal to base if nothing was committed
status: partial                  # done | partial | blocked | aborted
started: <RFC 3339 UTC>          # must match this file's name, converted
ended: <RFC 3339 UTC>
next: <what whoever continues does first, or none when status is done>
# worktrees: [<label>]
# supersedes: <file name>        # when this record corrects an earlier one
# blocked_by: [<job-id>]         # exactly when status is blocked
# touches: []                   # paths actually touched, relative to the code repository root
# metrics: {}                    # counts, coverage, durations
# artifacts: []                  # { path, sha256, bytes }, paths under job/<job-id>/
# assets: []                     # name@version
---

## What changed

## What was verified

## What remains

## Open questions

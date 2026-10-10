---
job: <job-id>                    # must equal the containing directory's name
status: active                   # active | done | abandoned; being blocked is derived
branch: job/<job-id>
owned_paths: []                  # paths this job touches, relative to the code repository root
owner: <actor>                   # optional; absent means unowned, and claimable by whoever takes it up
depends_on: []                   # optional; job ids that must reach `done` before work proceeds
keep: false                      # optional; true keeps this job's payload from collection
---

## Goal

## Scope

## Acceptance criteria

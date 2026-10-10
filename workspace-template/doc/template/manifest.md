---
asset: <asset-name>              # must equal the containing directory's name
version: <YYYY-MM-DD>            # must equal this file's name without .md
location: store                  # store, or an external locator such as s3://…
ref: <asset-name>@<version>      # the tag that publishes this version
sha256: <64 hex digits>          # omit both when the version is a directory: the git
bytes: <size in bytes>           # object the tag names is its identity, not a hash
# produced_by: { job: <job-id>, commit: <commit id> }
# source: <where externally sourced data came from>
# license: <the terms that govern reuse>
---

# <asset-name> <version>

## Description

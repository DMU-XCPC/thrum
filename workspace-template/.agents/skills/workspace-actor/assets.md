# Assets — `store/` and `doc/asset/`

How do durable bytes live, and how does a version become permanent? `store/` is where bytes that
must survive a job go. It is a **bare git repository**, and that is the whole design: git already
provides content addressing, immutability, deduplication, delta compression between similar
versions, integrity checking, transport and backup. A hand-rolled object directory would
reimplement all of it and lose the delta compression that makes comparing a dataset against a
future version cheap.


Assets are **never checked out permanently**. A job materialises the version it needs into its
own transient directory:

```sh
thrum asset materialise --asset uploader-baseline --version 2026-10-09 --job add-retry
# git -C store archive <ref> | tar -x -C job/<job-id>/data
```

Everything under `job/` is disposable, so the materialised copy inherits the retention policy of
the job that asked for it. That is deliberate: a version that has to persist is a version that
has been published, not one someone left on disk.

## Publishing is a tag

The blobs are written first, and the tag is what makes the version visible — which is what makes
publishing atomic. `thrum asset publish` writes the tag and the manifest together, as one
`asset` commit.

- **A tag is never moved and never deleted except by withdrawal.** It is the version's identity:
  moving one would silently repoint every manifest and every handoff that names it.
- A version that no tag reaches is regenerable by definition and may be collected
  (`thrum asset store` reports them).

## The manifest — `doc/asset/<asset-name>/<version>.md`

The manifest is the only part of an asset that is versioned. The bytes live in `store/` or
outside the workspace; the claim about them lives here.

```yaml
---
asset: uploader-baseline
version: 2026-10-09
location: store
ref: uploader-baseline@2026-10-09
sha256: 3f9c1d8b2a4e6f0c8a2e4b6d8f0a2c4e6b8d0f2a4c6e8b0d2f4a6c8e0b2d4f6a
bytes: 10737418240
produced_by: { job: add-retry-to-uploader, commit: 9be77d05 }
license: internal
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `asset` | always | must equal the containing directory name |
| `version` | always | must equal the file name without `.md` |
| `location` | always | `store`, or an external locator such as `s3://…` or `registry+https://…` |
| `ref` | when `location` is `store` | the tag that publishes this version |
| `sha256` | when the content is one blob, or lives outside git | the raw hash of the content |
| `bytes` | same as `sha256` | the size of the content |
| `produced_by` | optional | the `job` and `commit` that produced it; absent for externally sourced data |
| `source` | optional | where external data came from |
| `license` | optional | the terms that govern reuse |

The body has one required section, `## Description`, with additional sections allowed after it.
The document has exactly one H1 title, because it can be retired.

### Two hashes, and only one of them is the file's

Record the raw `sha256` as well as the ref. **A git object id is not a hash of the file**: git
hashes `blob <len>\0<content>`. Anything crossing the workspace boundary — a bundle, a handoff, a
message, a person with a download link — speaks in raw checksums, so both are recorded.

**What the checksums are computed over.** A version is a git object, which may be one blob or a
whole tree; `ref` names it either way, and git verifies it either way. `sha256` and `bytes` exist
for what leaves git's care — a single file, or a copy held somewhere else — so a version whose
content is a directory simply leaves them out. Computing one over a tar stream would only be
stable for as long as nothing about the tar ever changed, which is not a promise worth making.

### Identity freezes on publication

`location`, `ref`, and the checksum fields whenever they are present, freeze when the version is
published. Correcting a descriptive field — the description, the license — is allowed in place.
**Changing identity means a new version**, never an edit.

## Withdrawing a version

When a version turns out to be wrong, or must not be used, it is **retired like any other
document**, in one `retire` commit: the manifest moves to
`doc/archive/asset/<name>/<version>.md` with a notice, and the tag in `store/` is deleted, which
makes the bytes collectible.

Withdrawal is a retirement rather than a deletion, so a handoff that names the version keeps a
resolvable reference and the reason is on record. **The version number stays taken.**

## What does not belong here

> The workspace is a coordination plane, not a data lake.

`store/` is for durable bytes a job produced or needs, of a size and shape that git handles well.
It is not for:

| Not an asset here | Where it goes |
| --- | --- |
| Container images, model weights, terabyte-scale corpora | a registry or an object store, with an immutable locator in the manifest |
| Externally governed or licensed data | the system that governs it, with a locator and a `license` |
| Anything that must be deleted object by object | an external system; the workspace keeps a reference |
| Secrets | never — not in `doc/`, not in `store/`, not in any git repository |

Anything outside the workspace must be anchored by a locator inside it. That is the same rule as
"everything unversioned must be anchored by something versioned", extended past the boundary.

A bundle or a message attachment is **not** an asset: no content addressing, no tag, no manifest.
Sending bytes that way is transport, not a record — anything that has to survive is published and
referred to by name.

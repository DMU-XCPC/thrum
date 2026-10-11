# How a name is made

What may a name be? Names are identifiers, not descriptions. They are typed by people as often as
by tools, so they stay short and shell-safe; they are parsed by tools, so they follow one grammar;
and **they never encode state that can change**, because a name that has been written into a
record can never change again.

## The atoms

```text
slug  := [a-z0-9]+ ( "-" [a-z0-9]+ )*
stamp := YYYY "-" MM "-" DD "T" HH "-" MM "-" SS "Z"    # UTC, colons replaced by hyphens
```

## Every name in the workspace

| Name | Form | Example |
| --- | --- | --- |
| Job id | `slug` | `add-retry-to-uploader` |
| Branch | `job/` + job id | `job/add-retry-to-uploader` |
| Worktree label | `slug`, chosen for the **area of work** | `uploader` |
| Asset | `slug` | `uploader-baseline` |
| Asset version | `YYYY-MM-DD`, then `-N` for a further version that day | `2026-10-09`, `2026-10-09-2` |
| Asset tag | asset `@` version | `uploader-baseline@2026-10-09` |
| Handoff file | `stamp` + `.md` | `2026-10-09T02-10-00Z.md` |
| Convention file | two digits + `-` + `slug` + `.md` | `01-placement.md` |
| Topic | `slug` | `upload-protocol` |
| Topic document | `slug` + `.md` | `retry.md` |
| Actor | `slug` — one name for as long as that actor works | `agent-3` |

## The rules, and why each one exists

1. **One character set**: lowercase ASCII letters, digits and hyphens. No spaces, no uppercase,
   no underscores, and no dots outside file extensions.
2. **Short and shell-safe**, because people type them.
3. **Job ids, asset versions and convention numbers are taken forever** — within the workspace.
   Once used, none is reused, even after the job closes, the asset is withdrawn or the convention
   is retired. A job id's uniqueness is checked against both `doc/task/` and `doc/archive/`.
4. **Two things sit deliberately outside that rule.** A **worktree label** is reused by design,
   so its meaning must stay fixed and it is never repurposed for a different area of work. A
   **topic name** is not a register at all — topics are a knowledge tree, and they are added,
   retired and revived as knowledge moves.
5. **Names are reversible**: `job/` + job id ↔ job id; asset tag ↔ manifest path; handoff file
   name ↔ `started`; worktree label ↔ directory name. Reversibility is what lets a record be
   checked against the path it lives at.
6. **Names never describe state.** A job id carries no date, owner or status; a worktree label
   carries no occupant. A name that encoded state would go stale the moment the state changed,
   and it cannot be changed afterwards.
7. **`@` and `/` are reserved separators** and never appear inside a slug.
8. **Every name is at most 64 characters**, so it is safe as a single path component.
9. **Time has two forms**: file contents use RFC 3339 (`2026-10-09T02:10:00Z`) and names use the
   hyphen form (`2026-10-09T02-10-00Z`). They convert mechanically.

## Minting one

**A job id** is a slug for the work, and nothing else: `add-retry-to-uploader`, not
`2026-10-09-add-retry`. If the id is taken — in `doc/task/` or `doc/archive/` — choose another;
you never reuse one, and you never append a number to sneak past the check.

**A worktree label** names an **area** of work, never the job occupying it: `uploader`, not
`add-retry-worktree`. It is declared once, reused by every job that works on that area, and its
meaning is fixed forever. Wanting a different area is a new declaration, not a rename.

**A convention number is the highest in use plus one — never the first free one.** Numbers are
taken forever, so `convention/` may have gaps: a retired `01-placement.md` moves to
`doc/archive/convention/01-placement.md` and its number stays taken, because reusing it would
break every reference. A gap means "there was one, and it is archived".

**An asset version** is the date it was published, and `-2`, `-3` for further versions that day.
A version number that has been withdrawn is still taken: the identity of a version never moves,
or every manifest and every handoff naming it would silently repoint.

**An actor name** is one name for as long as that actor is working — every command of one session
uses the same one — because a handoff, an occupancy and a lease all record which actor did the
work. A later session may choose another, and the roster simply stops hearing from the old one. **The
name identifies the working session, not the machinery under it**: a model swapped mid-session, a
context compacted, a host restarted — and the session goes on writing under the same name, because
the records it already wrote are its own. That is also why neither a harness's name nor a model's
name is one: both are swapped while the session continues, and one harness hosts many sessions at
once.
An actor may be a person or an agent. The first time a name is used it exists; nothing is
registered, and nothing is refused because a name is missing — a name is not a permission.

**Not the name of the harness the actor runs in** — `deepseek-harness`, `dsh`, `claude-code`. A
harness is a changeable fact about the environment, so a name that encodes it goes stale, which is
the same reason no name here encodes state at all. It is also one harness to many actors: one such
name would make their handoffs, occupancies and leases read as one actor's, and the roster — which
is what the tooling has seen — could no longer tell them apart.

## What the rules buy

Because a name is reversible, a record can be checked against the path it lives at — a brief's
`job` against its directory, a handoff's `started` against its file name, a manifest's `asset`
and `version` against its directory and file. Because a name never encodes state, a check on it
cannot go stale. And because names are taken forever, *used once* is a property the tooling can
enforce rather than a promise everyone has to keep.

Out of scope: branch names the project owns (`main`, release branches), the name of the workspace
directory itself, and the name of a bundle, which is a transport artifact and not a name from
this register.

# How a name is made

What may a name be? Names are identifiers, not descriptions. They are typed by people as often as by
tools, so they stay short and shell-safe; they are parsed by tools, so they follow one grammar; and
**they never encode state that can change**, because a name that has been written into a record can
never change again.

## The atoms

```text
slug  := [a-z0-9]+ ( "-" [a-z0-9]+ )*
stamp := YYYY "-" MM "-" DD "T" HH "-" MM "-" SS "Z"    # UTC, colons replaced by hyphens
```

## Every name in the workspace

| Name | Form | Example |
| --- | --- | --- |
| Job id | `slug` | `add-retry-to-uploader` |
| Repository | `slug`, declared once | `code`, `web` |
| Branch | `job/` + job id | `job/add-retry-to-uploader` |
| Worktree label | `slug`, chosen for the **area of work** | `uploader` |
| Actor | `slug`, declared once and never reused | `dana`, `flash-9c2e` |
| Site | `slug` | `build-01` |
| Asset | `slug` | `uploader-baseline` |
| Asset version | `YYYY-MM-DD`, then `-N` for a further version that day | `2026-10-09`, `2026-10-09-2` |
| Asset tag | asset `@` version | `uploader-baseline@2026-10-09` |
| Handoff file | `stamp` + `.md` | `2026-10-09T02-10-00Z.md` |
| Convention file | two digits + `-` + `slug` + `.md` | `01-placement.md` |
| Topic | `slug` | `upload-protocol` |
| Topic document | `slug` + `.md` | `retry.md` |

## The rules, and why each one exists

1. **One character set**: lowercase ASCII letters, digits and hyphens. No spaces, no uppercase, no
   underscores, and no dots outside file extensions.
2. **Short and shell-safe**, because people type them.
3. **Job ids, repository names, actor names, site names, asset versions and convention numbers are
   taken forever** — within the workspace. Once used, none is reused, even after the job closes, the
   repository or the actor is retired, the asset is withdrawn or the convention is retired. A job
   id's uniqueness is checked against both `doc/task/` and `doc/archive/`.
4. **One thing sits deliberately outside that rule.** A **worktree label** is reused by design, so
   its meaning must stay fixed and it is never repurposed for a different area of work. A **topic
   name** is not a register at all: topics are a knowledge tree, added, retired and revived as
   knowledge moves.
5. **Names are reversible**: `job/` + job id ↔ job id; asset tag ↔ manifest path; handoff file name
   ↔ `started`; worktree label ↔ directory name. Reversibility is what lets a record be checked
   against the path it lives at.
6. **Names never describe state.** A job id carries no date, owner or status; a worktree label
   carries no occupant; an actor name carries no harness and no model. A name that encoded state
   would go stale the moment the state changed, and it could not be changed afterwards.
7. **`@` and `/` are reserved separators** and never appear inside a slug.
8. **Every name is at most 64 characters**, so it is safe as a single path component.
9. **Time has two forms**: file contents use RFC 3339 (`2026-10-09T02:10:00Z`) and names use the
   hyphen form (`2026-10-09T02-10-00Z`). They convert mechanically.

## Minting one

**A job id** is a slug for the work, and nothing else: `add-retry-to-uploader`, not
`2026-10-09-add-retry`. If the id is taken — in `doc/task/` or `doc/archive/` — choose another; you
never reuse one, and you never append a number to sneak past the check.

**A repository name** is the slug a brief's `repo` names and the directory `repos/<name>.git`
materialises: `code`, `web`. It is declared once, at `doc/repo/<name>.yaml`, and never reused.

**A worktree label** names an **area** of work, never the job occupying it: `uploader`, not
`add-retry-worktree`. It is declared once, at `doc/worktree/<label>.yaml`, reused by every job that
works on that area, and its meaning is fixed forever. Wanting a different area is a new declaration,
not a rename.

**A convention number is the highest in use plus one — never the first free one.** Numbers are taken
forever, so `convention/` may have gaps: a retired `01-placement.md` moves to
`doc/archive/convention/01-placement.md` and its number stays taken. A gap means "there was one, and
it is archived".

**An asset version** is the date it was published, and `-2`, `-3` for further versions that day. A
withdrawn version number is still taken: the identity of a version never moves, or every manifest
and every handoff naming it would silently repoint.

**An actor name is declared once and belongs to one actor.** It is not a session and not a process:
a name is a person or an agent, and it is the same name tomorrow — `actors.md` has the rest. **It
names this actor, and nothing that fits everybody**: a project's name (`agent-novaphy`), a task, an
area of work, a machine, the bare harness (`deepseek-harness`, `dsh`, `claude-code`) and the bare
model (`deepseek-flash`, `gpt-5`) are names any actor here could take, and records under them read
as one actor's. The bare model is the tempting one: every session running it would claim the same
name.

**An agent that has to pick its own name mints it in one shape** — the model in a word, a hyphen,
and four random characters:

```text
flash-9c2e          # deepseek-flash; the suffix is this actor's
gpt5-3w8q           # another model, another suffix
word   := one short word for the model family   # flash, gpt5
suffix := exactly four characters from [a-z0-9]
```

- **The suffix is the identity; the word is a label.** The suffix is what makes the name this
  actor's, and it is never reused. The word only tells a reader what kind of thing you are: chosen
  once, not updated, and not a claim about what is running now — a name does not change when the
  model does. Use the model you are running (`flash`, `gpt5`), not a generic word: `agent-1dcd` is
  legal, but it is not this shape and says nothing a reader can use.
- **Four characters is enough here** — a workspace holds a few dozen actors — and
  `thrum actor list` is the collision check: look before you mint. `LC_ALL=C tr -dc 'a-z0-9'
  </dev/urandom | head -c 4` is one way to draw the suffix.
- **One shape every time** is the point: short, typed without thought, and recognisable as an
  agent's name rather than a person's, a project's or a model's.

**A harness and a model are changeable facts about the environment**, and one harness hosts many
actors at once: that is why the word in a name is a label chosen once and never a description kept
current. Look at `thrum actor list` before taking an invented name; a name already there is
somebody's.

## What the rules buy

Because a name is reversible, a record can be checked against the path it lives at — a brief's `job`
against its directory, a handoff's `started` against its file name, a manifest's `asset` and
`version` against its directory and file. Because a name never encodes state, a check on it cannot
go stale. And because names are taken forever, *used once* is a property thrum can enforce rather
than a promise everyone has to keep.

Out of scope: branch names the project owns (`main`, release branches), the name of the workspace
directory itself, and the name of a bundle, which is a transport artifact and not a name from this
register.

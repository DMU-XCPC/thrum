# Documents

| File | What it is |
| --- | --- |
| `design.md` | The design of a workspace: what it is made of, what rules hold it together, and why. The authority for everything else in this repository. |
| `tooling.md` | The interface: the operations, what each does and refuses, and every command, kept in step with the binary by a test. It adds no rules of its own. |
| `implementation.md` | How the tooling is built: the stack, the record line and its lock, the daemon's process. It adds no rules either. |

These are documents about the workspace, kept in the development repository. A workspace
created from the template has a `doc/` of its own, and that one holds **records** —
briefs, topics, conventions, asset manifests — rather than documents about the tooling.
The two are unrelated beyond the name.

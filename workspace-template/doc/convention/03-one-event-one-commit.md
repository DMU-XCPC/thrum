# One commit is one event

Every commit in the coordination repository is one event, and its subject names it: the
kind, its subject, and what changed.

```text
<kind> <subject>: <summary>
```

Record it through the tooling, so the form is right and the line stays what it is.

**The records are one line, with no merges.** A commit that loses a race is taken back
out and recorded again on top of what moved; a commit whose file moved as well is refused
rather than merged. So: never force-push the records, never merge them, and never rewrite
a commit that has been published.

**Never run a bare `git clean` at the workspace root.** `git clean -xdf` would delete
`.bare/` and the entire source history with it. Use `thrum clean`.

A message says what changed, not what the document says. The document is the content, and
restating it is how a message goes stale.

The code repository's commit style is the project's own business. Nothing here constrains
it.

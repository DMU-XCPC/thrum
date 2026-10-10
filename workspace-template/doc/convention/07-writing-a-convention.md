# Writing a convention

A convention is a rule an agent must read. Conventions are read in number order, so the
order is the priority: the rule that must not be missed comes first.

- **The file is `doc/convention/<nn>-<slug>.md`** — two digits, a hyphen, a slug. The slug
  says what the rule is about and never what state it is in.
- **Numbers are taken forever.** A new convention takes the highest number in use plus
  one, never the first free one: filling a gap would reuse a number that a record may
  already name. A gap means there was a convention there, and it is archived.
- **One H1, then the rule** in the imperative, with the reason it exists. A rule whose
  reason is missing is a rule somebody will delete by accident.
- **A convention instructs; a topic describes.** If what you are writing explains rather
  than obliges, it belongs in `doc/topic/`.
- **Retire, never delete.** Move the file to `doc/archive/convention/` under the same
  name, and add a retirement notice under the title saying what replaced it — or that
  nothing did. The number stays taken.
- **A value a tool has to read goes in one `values` block**, whose keys are the name the
  tool looks for rather than a bare noun:

  ```values
  collect.window: 14d
  ```

  Values are scalars. Two conventions must not define the same key, because then the
  workspace says two things at once. A key no tool reads is a key for the reader, which is
  allowed and is how a project records a value it intends a tool to read later.

Change a convention the way you change anything else here: in an editor, then recorded
with `thrum commit`.

// Package convention reads the rules an agent must read, and the few values inside them
// that a tool has to act on.
//
// A convention instructs where a topic describes, and it is the only place a project's
// policy reaches the tooling: how long a payload is kept, for instance, is a decision
// everyone in the workspace should see the same way and nobody should have to guess. So a
// convention may carry one fenced `values` block, and its keys name the tool and the value
// — `collect.window`, not `window` — because the file it lives in is named after the rule
// rather than after whichever tool happens to read it.
//
// Reading is deliberately total: every problem is reported rather than thrown, because one
// convention nobody can parse must not stop the tool from acting on the others, and a
// policy that is silently ignored is worse than one that is loudly wrong.
package convention

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Values is what the conventions of a workspace say that a tool has to read.
type Values map[string]string

// Problem is a convention that does not read the way it is written.
type Problem struct {
	Where string // workspace-relative path
	What  string
}

func (p Problem) String() string { return p.Where + ": " + p.What }

// keyRE is what a value's key looks like: a dotted slug, so that the first part names
// what reads it and two conventions cannot collide by accident.
var keyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)+$`)

// known is what this tooling reads, and the shape it reads it in. Which keys exist is the
// tooling's knowledge, so it is also the tooling's job to say when one of them is written
// in a shape it cannot read. A key that is not here is a key for the reader: a project may
// record a value it intends a later version to read, and that is not an error.
var known = map[string]string{
	"collect.window": "a length of time, like 14d or 336h",
}

// Read returns every value this workspace's conventions declare, and one problem for
// every convention that could not be read the way it is written.
//
// Two conventions defining the same key is a problem rather than a precedence rule: the
// workspace would be saying two things at once, and which one a tool acted on would depend
// on the order the files happen to be read in.
func Read(l workspace.Layout) (Values, []Problem, error) {
	paths, err := filepath.Glob(filepath.Join(l.Convention(), "*.md"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)

	values := Values{}
	where := map[string]string{}
	var problems []Problem

	for _, path := range paths {
		rel, err := filepath.Rel(l.Root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		blocks, errs := blocks(string(raw))
		for _, what := range errs {
			problems = append(problems, Problem{Where: rel, What: what})
		}
		for _, block := range blocks {
			declared := map[string]any{}
			if err := yaml.Unmarshal([]byte(block), &declared); err != nil {
				problems = append(problems, Problem{Where: rel, What: "the values block is not YAML: " + err.Error()})
				continue
			}
			keys := make([]string, 0, len(declared))
			for key := range declared {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if !keyRE.MatchString(key) {
					problems = append(problems, Problem{Where: rel,
						What: fmt.Sprintf("the value %q is not a name a tool can look for: a key is a dotted slug, like collect.window", key)})
					continue
				}
				text, ok := scalar(declared[key])
				if !ok {
					problems = append(problems, Problem{Where: rel,
						What: fmt.Sprintf("the value %q is not a scalar: a value is one thing a tool can read", key)})
					continue
				}
				if first, dup := where[key]; dup {
					problems = append(problems, Problem{Where: rel,
						What: fmt.Sprintf("%s is declared here and in %s: the workspace cannot say two things at once", key, first)})
					continue
				}
				if shape, isKnown := known[key]; isKnown {
					if err := readable(key, text, shape); err != nil {
						// Reported, and kept: a tool that reads this value must fail
						// rather than quietly use its own default, which would hide the
						// mistake that made the policy unreadable.
						problems = append(problems, Problem{Where: rel, What: err.Error()})
					}
				}
				where[key] = rel
				values[key] = text
			}
		}
	}
	return values, problems, nil
}

// Duration reads a value that is a length of time, and returns the default when the
// workspace says nothing about it.
//
// A value that is not a duration is an error rather than a silent fallback: a policy
// nobody can read is a policy nobody has, and quietly using the default would hide the
// mistake that made it unreadable. Days are accepted because policy is written in them —
// `14d` as well as `336h` — and everything else is what Go already parses.
func (v Values) Duration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := v[key]
	if !ok {
		return def, nil
	}
	d, err := parseDuration(raw)
	if err != nil {
		return def, fmt.Errorf("the convention value %s is %q, which is not a length of time", key, raw)
	}
	return d, nil
}

// readable checks a known key against the shape the tooling reads it in.
func readable(key, text, shape string) error {
	if key == "collect.window" {
		if _, err := parseDuration(text); err != nil {
			return fmt.Errorf("%s is %q, and %s reads it as %s", key, text, "this tooling", shape)
		}
	}
	return nil
}

func parseDuration(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if days, ok := strings.CutSuffix(text, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(text)
}

// blocks returns the contents of the `values` fences in a convention, and one message for
// every way the file fails to have exactly one well-formed block.
func blocks(text string) ([]string, []string) {
	var (
		out  []string
		errs []string
		open bool
		body []string
	)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		switch {
		// A declaration starts at the left margin. An indented fence is what a fence
		// inside a list item looks like, which is exactly how an example of one is
		// written — and an example must not declare anything.
		case !open && trimmed == "```values":
			open = true
			body = nil
		case open && strings.TrimSpace(trimmed) == "```":
			open = false
			out = append(out, strings.Join(body, "\n"))
		case open:
			body = append(body, line)
		}
	}
	if open {
		errs = append(errs, "a values block is not closed")
	}
	if len(out) > 1 {
		errs = append(errs, "a convention carries one values block, and this one has more: a value belongs with the one rule that uses it")
	}
	return out, errs
}

// scalar turns a YAML scalar into the text a tool reads, and refuses anything that is not
// one: a mapping or a list is a document, not a value.
func scalar(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", true
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	default:
		return "", false
	}
}

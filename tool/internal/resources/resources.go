// Package resources declares the things a workspace shares and leases.
//
// A registry is the one file that holds several entries, and it is edited by node
// rather than by decoding and re-encoding: a project adds fields of its own, and a
// field the tooling does not read is a field for the reader, so rewriting the file
// must not throw it away.
package resources

import (
	"fmt"
	"os"
	"sort"

	"github.com/multiagentworkspace/thrum/tool/internal/atomic"
	"github.com/multiagentworkspace/thrum/tool/internal/commit"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
	"gopkg.in/yaml.v3"
)

// Entry is what declaring a resource needs.
type Entry struct {
	Name      string
	Purpose   string
	Capacity  int
	Ephemeral *bool
	// Extra holds fields the tooling does not read. They belong to the reader.
	Extra map[string]string
}

// Declare adds an entry to a kind's registry, creating the file when the kind has not
// been declared before.
func Declare(l workspace.Layout, kind string, e Entry) error {
	// Nothing is read or written before the line is current: a declaration is a decision
	// about what exists, and it has to be made on what the others have published.
	if err := commit.Current(l); err != nil {
		return err
	}
	if !format.ValidSlug(kind) {
		return fmt.Errorf("%q is not a kind: a kind is a slug", kind)
	}
	if !format.ValidSlug(e.Name) {
		return fmt.Errorf("%q is not a resource name: a name is a slug", e.Name)
	}
	if e.Purpose == "" {
		return fmt.Errorf("a resource needs a purpose: one line saying what it is for")
	}
	if err := os.MkdirAll(l.Resource(), 0o755); err != nil {
		return err
	}

	// A name is unique across every kind, not only within one: a lease refers to a unit by
	// naming a resource, so a name that meant two things would make the lease refer to
	// whichever entry the reader happened to reach first. The tooling is the only writer of
	// these files, and writing a registry its own verify calls broken is not a thing it
	// should be able to do.
	declared, err := All(l)
	if err != nil {
		return err
	}
	for other, entries := range declared {
		if other == kind {
			continue // the write below refuses a duplicate within the kind, in its own words
		}
		if _, taken := entries[e.Name]; taken {
			return fmt.Errorf("%q is already declared as a %s: a resource name is unique across every kind", e.Name, other)
		}
	}

	path := l.ResourceKind(kind)
	return atomic.Replace(path, func(current []byte) ([]byte, error) {
		doc, err := parse(current, kind)
		if err != nil {
			return nil, err
		}
		seq, err := sequence(doc, kind)
		if err != nil {
			return nil, err
		}
		for _, item := range seq.Content {
			if nameOf(item) == e.Name {
				return nil, fmt.Errorf("%s already declares %q", path, e.Name)
			}
		}
		seq.Content = append(seq.Content, entryNode(e))
		return encode(doc)
	})
}

// Remove deletes an entry, and the file with it when it held nothing else.
//
// Removing a resource whose directory exists is refused rather than permitted:
// pulling a working directory out from under whatever is using it is not a thing a
// permission makes safe.
func Remove(l workspace.Layout, kind, name string) error {
	if err := commit.Current(l); err != nil {
		return err
	}
	path := l.ResourceKind(kind)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s declares nothing", kind)
	}

	held, err := materialised(l, kind, name)
	if err != nil {
		return err
	}
	if held != "" {
		return fmt.Errorf("%s is materialised at %s: it cannot be removed while something is using it", name, held)
	}

	return atomic.Replace(path, func(current []byte) ([]byte, error) {
		doc, err := parse(current, kind)
		if err != nil {
			return nil, err
		}
		seq, err := sequence(doc, kind)
		if err != nil {
			return nil, err
		}
		found := false
		kept := seq.Content[:0]
		for _, item := range seq.Content {
			if nameOf(item) == name {
				found = true
				continue
			}
			kept = append(kept, item)
		}
		if !found {
			return nil, fmt.Errorf("%s declares no %q", kind, name)
		}
		seq.Content = kept

		if len(kept) == 0 {
			// A kind's file exists only while the kind declares something, and an
			// empty mapping would fail validation for having no kind key at all.
			return nil, nil
		}
		return encode(doc)
	})
}

// materialised reports where a resource exists on disk, if it does. Only some kinds
// have anything on disk: a working directory has its directory, and a service has its
// state.
func materialised(l workspace.Layout, kind, name string) (string, error) {
	switch kind {
	case format.WorktreeKind:
		dir := l.Worktree(name)
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	case "service":
		dir := l.State() + "/" + name
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}
	return "", nil
}

// All returns every declared resource, by kind and then by name.
func All(l workspace.Layout) (map[string]map[string]format.Resource, error) {
	out := map[string]map[string]format.Resource{}
	paths, err := os.ReadDir(l.Resource())
	if err != nil {
		return out, nil // nothing declared yet
	}
	for _, p := range paths {
		if p.IsDir() || len(p.Name()) < 6 || p.Name()[len(p.Name())-5:] != ".yaml" {
			continue
		}
		kind := p.Name()[:len(p.Name())-5]
		entries, err := workspace.ReadResources(l, kind)
		if err != nil {
			return nil, err
		}
		out[kind] = entries
	}
	return out, nil
}

// Kinds lists the declared kinds, in order.
func Kinds(l workspace.Layout) ([]string, error) {
	all, err := All(l)
	if err != nil {
		return nil, err
	}
	kinds := make([]string, 0, len(all))
	for k := range all {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds, nil
}

// parse reads a registry into a document node, or makes an empty one for a kind that
// has never been declared.
func parse(current []byte, kind string) (*yaml.Node, error) {
	if len(current) > 0 {
		var doc yaml.Node
		if err := yaml.Unmarshal(current, &doc); err != nil {
			return nil, err
		}
		if doc.Kind == 0 || len(doc.Content) == 0 {
			return nil, fmt.Errorf("a registry file holds a mapping of kind to entries")
		}
		return &doc, nil
	}

	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kind}
	key.HeadComment = fmt.Sprintf("Declared %s, and what each is for.", kind)
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, seq}}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}
	return doc, nil
}

// sequence finds the sequence a kind's entries live in, making it when the file holds
// some other kind.
func sequence(doc *yaml.Node, kind string) (*yaml.Node, error) {
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("a registry file holds a mapping of kind to entries")
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == kind {
			seq := mapping.Content[i+1]
			if seq.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%q does not hold a list of entries", kind)
			}
			return seq, nil
		}
	}
	// The file holds other kinds but not this one yet.
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kind}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping.Content = append(mapping.Content, key, seq)
	return seq, nil
}

// entryNode renders one entry, core fields first and the reader's fields after.
func entryNode(e Entry) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(key string, value *yaml.Node) {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	add("name", str(e.Name))
	add("purpose", str(e.Purpose))
	if e.Capacity > 0 {
		add("capacity", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(e.Capacity)})
	}
	if e.Ephemeral != nil {
		add("ephemeral", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(*e.Ephemeral)})
	}
	for _, k := range sortedKeys(e.Extra) {
		add(k, scalarFor(e.Extra[k]))
	}
	return m
}

func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// scalarFor guesses a type for a field the tooling does not read, so that a number
// stays a number and everything else stays a string.
func scalarFor(s string) *yaml.Node {
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err == nil && fmt.Sprint(i) == s {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: s}
	}
	if s == "true" || s == "false" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: s}
	}
	return str(s)
}

func encode(doc *yaml.Node) ([]byte, error) {
	return format.MarshalYAML(doc)
}

// nameOf reads the name field of an entry, which is the one field every kind has.
func nameOf(entry *yaml.Node) string {
	if entry == nil || entry.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(entry.Content); i += 2 {
		if entry.Content[i].Value == "name" {
			return entry.Content[i+1].Value
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

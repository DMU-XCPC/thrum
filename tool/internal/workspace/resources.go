package workspace

import (
	"fmt"
	"os"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
)

// ReadResources returns the entries a registry declares for one kind.
//
// The registry is decoded loosely, because a project adds fields of its own and a
// field the tooling does not read is a field for the reader.
func ReadResources(l Layout, kind string) (map[string]format.Resource, error) {
	out := map[string]format.Resource{}
	raw, err := os.ReadFile(l.ResourceKind(kind))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var file map[string][]format.Resource
	if err := format.DecodeLoose(raw, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", l.ResourceKind(kind), err)
	}
	entries, ok := file[kind]
	if !ok {
		return nil, fmt.Errorf("%s has no top-level key %q", l.ResourceKind(kind), kind)
	}
	for _, e := range entries {
		out[e.Name] = e
	}
	return out, nil
}

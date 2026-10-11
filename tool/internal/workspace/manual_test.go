package workspace_test

import (
	"io/fs"
	"reflect"
	"strings"
	"testing"

	root "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/bundles"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/messages"
)

// The manual is the only place inside a workspace where the record formats are written down, so
// a field the tooling decodes and the manual never names is a field no actor can use. The
// formats are also the one part of the manual that can be checked against the code rather than
// against a memory of the design — and adding a field is exactly the change that would otherwise
// leave the manual behind without anything noticing.
//
// This does not check that the manual is complete. It checks that the half of it which can be
// compared mechanically still agrees with what the code reads.
func TestTheManualNamesEveryFieldTheFormatsDecode(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"records.md", "assets.md", "claims.md", "sites.md"} {
		b, err := fs.ReadFile(root.Template, root.TemplateDir+"/.agents/skills/workspace-actor/"+name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}

	for _, c := range []struct {
		file string
		typ  any
	}{
		{"records.md", format.Brief{}},   // doc/task/<job-id>/brief.md
		{"records.md", format.Handoff{}}, // handoff/<job-id>/<stamp>.md
		{"records.md", format.Artifact{}},
		{"assets.md", format.Manifest{}},   // doc/asset/<name>/<version>.md
		{"assets.md", format.ProducedBy{}}, //
		{"claims.md", format.Lease{}},      // var/lease/<resource>.<unit>.yaml
		{"claims.md", format.Occupy{}},     // var/occupy/<job-id>.yaml
		{"claims.md", format.Resource{}},   // doc/resource/<kind>.yaml
		{"sites.md", messages.Message{}},   // var/msg/<actor>/<id>.md
		{"sites.md", messages.Attachment{}},
		{"sites.md", bundles.Header{}}, // bundle.yaml
	} {
		name := reflect.TypeOf(c.typ).Name()
		for _, field := range yamlFields(c.typ) {
			if !strings.Contains(files[c.file], "`"+field+"`") {
				t.Errorf("%s documents %s and never names the field %q", c.file, name, field)
			}
		}
	}
}

// yamlFields are the names a record type decodes from its front-matter.
func yamlFields(v any) []string {
	var out []string
	t := reflect.TypeOf(v)
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		out = append(out, strings.Split(tag, ",")[0])
	}
	return out
}

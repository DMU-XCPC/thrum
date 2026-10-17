// Package retire moves a document to the archive with a notice: the record of a
// decision that is no longer current, kept because the reason is worth keeping.
package retire

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
)

// Retire moves doc/<rel> to doc/archive/<rel> and adds a notice under its title.
func Retire(l layout.Layout, rel, why, supersededBy string, now time.Time) error {
	rel = filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(rel), "doc/"))
	if !allowed(rel) {
		return fmt.Errorf("%s is not a document that retires: use topic/, convention/ or asset/", rel)
	}
	if why == "" {
		return fmt.Errorf("--why is required: a retirement states its reason")
	}

	src := filepath.Join(l.Doc(), filepath.FromSlash(rel))
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("no document at doc/%s", rel)
	}
	dst := filepath.Join(l.Archive(), filepath.FromSlash(rel))
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("doc/archive/%s already exists", rel)
	}

	notice := fmt.Sprintf("> Retired %s: %s.\n> %s",
		now.UTC().Format(format.DayForm), strings.TrimSuffix(why, "."), replacement(supersededBy))

	if st.IsDir() {
		if err := addNotice(filepath.Join(src, "overview.md"), notice); err != nil {
			return err
		}
	} else if err := addNotice(src, notice); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{src, dst},
		fmt.Sprintf("retire %s: %s", strings.TrimSuffix(rel, ".md"), why))
}

func allowed(rel string) bool {
	for _, prefix := range []string{"topic/", "convention/", "asset/"} {
		if strings.HasPrefix(rel, prefix) && len(rel) > len(prefix) {
			return true
		}
	}
	return false
}

func replacement(path string) string {
	if path == "" {
		return "No replacement."
	}
	return "Superseded by `" + strings.TrimPrefix(filepath.ToSlash(path), "doc/") + "`."
}

// addNotice puts the notice immediately after the document's one H1 title.
func addNotice(path, notice string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	front, body, splitErr := format.Split(data)
	if splitErr != nil {
		front, body = nil, data
	}

	var out []string
	inserted := false
	for _, ln := range strings.Split(string(body), "\n") {
		out = append(out, ln)
		if !inserted && strings.HasPrefix(ln, "# ") {
			out = append(out, "", notice)
			inserted = true
		}
	}
	if !inserted {
		return fmt.Errorf("%s has no H1 title to put the notice under", path)
	}
	joined := []byte(strings.Join(out, "\n"))
	if front == nil {
		return format.WriteFileAtomic(path, joined)
	}
	return format.SaveDocument(path, front, joined)
}

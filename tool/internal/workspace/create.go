package workspace

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	thrum "github.com/multiagentworkspace/thrum"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/gitx"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// CodeSource is one code repository a new workspace starts with.
type CodeSource struct {
	Name string
	Path string
}

// CreateOptions is everything create needs.
type CreateOptions struct {
	Root string
	Site string
	Code []CodeSource
}

type siteDecl struct {
	Site  string `yaml:"site"`
	Key   string `yaml:"key"`
	Reach string `yaml:"reach,omitempty"`
}

type repoDecl struct {
	Repo   string `yaml:"repo"`
	About  string `yaml:"about,omitempty"`
	Origin string `yaml:"origin,omitempty"`
}

// Create instantiates a workspace: the line, the ledger, one bare repository per
// declared code repository, the store, the runtime directories, and the skeleton.
func Create(opts CreateOptions) error {
	if !format.ValidSlug(opts.Site) {
		return fmt.Errorf("%q is not a site name: a site name is a slug", opts.Site)
	}
	seen := make(map[string]bool, len(opts.Code))
	for _, c := range opts.Code {
		if !format.ValidSlug(c.Name) {
			return fmt.Errorf("%q is not a repository name: a repository name is a slug", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("repository %q is named twice", c.Name)
		}
		seen[c.Name] = true
	}

	if entries, err := os.ReadDir(opts.Root); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", opts.Root)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(opts.Root, 0o755); err != nil {
		return err
	}
	l := layout.New(opts.Root)

	// The skeleton, and the ignore rules that keep it out of the line.
	if err := writeSkeleton(l); err != nil {
		return err
	}
	ignore, err := ignoreList()
	if err != nil {
		return err
	}
	if err := os.WriteFile(l.GitIgnore(), ignore, 0o644); err != nil {
		return err
	}

	// The line.
	if err := gitx.OK(l.Root, "init", "-b", "main", "."); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.InfoExclude()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(l.InfoExclude(), ignore, 0o644); err != nil {
		return err
	}
	if err := copyDocTemplates(l); err != nil {
		return err
	}

	// Everything a repository cannot carry.
	for _, d := range l.EmptyDirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	pub, err := writeSiteKey(l)
	if err != nil {
		return err
	}
	if err := writeDecl(l.Site(opts.Site), siteDecl{Site: opts.Site, Key: pub}); err != nil {
		return err
	}
	for _, c := range opts.Code {
		if err := writeDecl(l.RepoDecl(c.Name), repoDecl{Repo: c.Name}); err != nil {
			return err
		}
	}
	if err := os.WriteFile(l.SiteFile(), []byte(opts.Site+"\n"), 0o644); err != nil {
		return err
	}

	if err := commitAll(l.Root, "init: created from the template"); err != nil {
		return err
	}

	// The ledger: its own history, and its own first event.
	if err := gitx.OK(l.Ledger(), "init", "-b", "main", "."); err != nil {
		return err
	}
	if err := commitAllEmpty(l.Ledger(), "init: the ledger"); err != nil {
		return err
	}

	// The bare repositories.
	if err := gitx.OK("", "init", "--bare", l.Store()); err != nil {
		return err
	}
	for _, c := range opts.Code {
		if c.Path != "" {
			if err := gitx.OK("", "clone", "--bare", c.Path, l.CodeRepo(c.Name)); err != nil {
				return err
			}
		} else if err := gitx.OK("", "init", "--bare", l.CodeRepo(c.Name)); err != nil {
			return err
		}
	}
	return nil
}

// writeSkeleton materialises the entry point and the skill from the template.
func writeSkeleton(l layout.Layout) error {
	for _, f := range layout.SkeletonFiles() {
		data, err := fs.ReadFile(thrum.Template, thrum.TemplateDir+"/"+f.Path)
		if err != nil {
			return fmt.Errorf("template: %w", err)
		}
		dst := filepath.Join(l.Root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ignoreList is the template's ignore rules followed by one per skeleton file: the
// skeleton is unversioned, and a rule that is missing here is a file somebody commits
// by accident.
func ignoreList() ([]byte, error) {
	base, err := fs.ReadFile(thrum.Template, thrum.TemplateDir+"/.gitignore")
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	var b bytes.Buffer
	b.Write(base)
	if len(base) > 0 && base[len(base)-1] != '\n' {
		b.WriteByte('\n')
	}
	for _, f := range layout.SkeletonFiles() {
		b.WriteString(f.Ignore())
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// copyDocTemplates copies the copy-me files into the line, where they are versioned.
func copyDocTemplates(l layout.Layout) error {
	dir := thrum.TemplateDir + "/doc/template"
	entries, err := fs.ReadDir(thrum.Template, dir)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	if err := os.MkdirAll(l.DocTemplates(), 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(thrum.Template, dir+"/"+e.Name())
		if err != nil {
			return fmt.Errorf("template: %w", err)
		}
		if err := os.WriteFile(filepath.Join(l.DocTemplates(), e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeSiteKey(l layout.Layout) (string, error) {
	seed, pub, err := GenerateSiteKey()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(l.SecretsDir(), 0o700); err != nil {
		return "", err
	}
	body := base64.StdEncoding.EncodeToString(seed) + "\n"
	if err := os.WriteFile(l.PrivateKey(), []byte(body), 0o600); err != nil {
		return "", err
	}
	return pub, nil
}

func writeDecl(path string, v any) error {
	data, err := format.MarshalYAML(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return format.WriteFileAtomic(path, data)
}

func commitAll(dir, message string) error {
	identity := gitx.IdentityArgs(dir, "thrum")
	add := append(append([]string{}, identity...), "add", "-A")
	if err := gitx.OK(dir, add...); err != nil {
		return err
	}
	commit := append(append([]string{}, identity...), "commit", "-m", message)
	return gitx.OK(dir, commit...)
}

func commitAllEmpty(dir, message string) error {
	identity := gitx.IdentityArgs(dir, "thrum")
	args := append(append([]string{}, identity...), "commit", "--allow-empty", "-m", message)
	return gitx.OK(dir, args...)
}

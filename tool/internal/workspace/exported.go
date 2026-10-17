package workspace

import "github.com/multiagentworkspace/thrum/tool/internal/layout"

// The exported half of what create uses, for join: the same skeleton, ignore rules,
// declarations and commits, so the two paths cannot drift.

// WriteSkeleton materialises the entry point and the skill from the template.
func WriteSkeleton(l layout.Layout) error { return writeSkeleton(l) }

// IgnoreList is the template's rules followed by one per skeleton file.
func IgnoreList() ([]byte, error) { return ignoreList() }

// WriteDecl writes a pure-YAML declaration atomically.
func WriteDecl(path string, v any) error { return writeDecl(path, v) }

// CommitAll stages everything and commits it, with the identity fallback.
func CommitAll(dir, message string) error { return commitAll(dir, message) }

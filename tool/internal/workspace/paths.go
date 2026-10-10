// Package workspace is the layout of a workspace and the operations that read and
// write one. Every path the design names is built here, so that no other package has
// to know the tree by heart.
package workspace

import (
	"path/filepath"
)

// A Layout is a workspace root.
type Layout struct {
	Root string
}

// New returns the layout rooted at root.
//
// The root is made absolute, and that is not cosmetic: git is invoked as
// `git -C <root> ... <path>` in several operations, and git resolves a relative path
// argument against the directory -C names rather than against the caller's, which
// would put a relative root's repositories one level too deep.
func New(root string) Layout {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return Layout{Root: abs}
}

// Coordination repository.
func (l Layout) GitIgnore() string { return filepath.Join(l.Root, ".gitignore") }

// Code repository, bare, checked out into worktrees.
func (l Layout) CodeRepo() string { return filepath.Join(l.Root, ".bare") }

// Asset repository, bare, never checked out.
func (l Layout) Store() string { return filepath.Join(l.Root, "store") }

// Coordination plane.
func (l Layout) Doc() string                   { return filepath.Join(l.Root, "doc") }
func (l Layout) Convention() string            { return filepath.Join(l.Doc(), "convention") }
func (l Layout) Task() string                  { return filepath.Join(l.Doc(), "task") }
func (l Layout) Topic() string                 { return filepath.Join(l.Doc(), "topic") }
func (l Layout) Asset() string                 { return filepath.Join(l.Doc(), "asset") }
func (l Layout) DocTemplate() string           { return filepath.Join(l.Doc(), "template") }
func (l Layout) Archive() string               { return filepath.Join(l.Doc(), "archive") }
func (l Layout) Resource() string              { return filepath.Join(l.Doc(), "resource") }
func (l Layout) Handoff() string               { return filepath.Join(l.Root, "handoff") }
func (l Layout) TaskDir(job string) string     { return filepath.Join(l.Task(), job) }
func (l Layout) Brief(job string) string       { return filepath.Join(l.TaskDir(job), "brief.md") }
func (l Layout) ArchiveTask(job string) string { return filepath.Join(l.Archive(), "task", job) }
func (l Layout) ArchivedBrief(job string) string {
	return filepath.Join(l.ArchiveTask(job), "brief.md")
}
func (l Layout) HandoffDir(job string) string { return filepath.Join(l.Handoff(), job) }
func (l Layout) HandoffFile(job, name string) string {
	return filepath.Join(l.HandoffDir(job), name)
}
func (l Layout) AssetDir(asset string) string { return filepath.Join(l.Asset(), asset) }
func (l Layout) Manifest(asset, version string) string {
	return filepath.Join(l.AssetDir(asset), version+".md")
}
func (l Layout) ArchivedManifest(asset, version string) string {
	return filepath.Join(l.Archive(), "asset", asset, version+".md")
}
func (l Layout) ResourceKind(kind string) string {
	return filepath.Join(l.Resource(), kind+".yaml")
}

// Runtime plane.
func (l Layout) Var() string      { return filepath.Join(l.Root, "var") }
func (l Layout) Lease() string    { return filepath.Join(l.Var(), "lease") }
func (l Layout) Occupy() string   { return filepath.Join(l.Var(), "occupy") }
func (l Layout) State() string    { return filepath.Join(l.Var(), "state") }
func (l Layout) Env() string      { return filepath.Join(l.Var(), "env") }
func (l Layout) Cache() string    { return filepath.Join(l.Var(), "cache") }
func (l Layout) Config() string   { return filepath.Join(l.Var(), "config") }
func (l Layout) Secrets() string  { return filepath.Join(l.Var(), "secrets") }
func (l Layout) SiteFile() string { return filepath.Join(l.Var(), "site") }
func (l Layout) LeaseFile(resource, unit string) string {
	return filepath.Join(l.Lease(), resource+"."+unit+".yaml")
}
func (l Layout) OccupyFile(job string) string {
	return filepath.Join(l.Occupy(), job+".yaml")
}
func (l Layout) DaemonSocket() string { return filepath.Join(l.Var(), "daemon.sock") }
func (l Layout) DaemonLock() string   { return filepath.Join(l.Var(), "daemon.lock") }

// A job's payload.
func (l Layout) Jobs() string             { return filepath.Join(l.Root, "job") }
func (l Layout) JobDir(job string) string { return filepath.Join(l.Jobs(), job) }
func (l Layout) JobSub(job, sub string) string {
	return filepath.Join(l.JobDir(job), sub)
}

// Payload subdirectories, in the order the design lists them.
var JobSubdirs = []string{"log", "tmp", "cache", "data", "state", "ckpt", "out"}

// Labelled working directories.
func (l Layout) Worktrees() string { return filepath.Join(l.Root, "worktree") }
func (l Layout) Worktree(label string) string {
	return filepath.Join(l.Worktrees(), label)
}

// EmptyDirs are the directories a workspace needs and a repository cannot carry,
// which is why creating a workspace is not a plain file copy.
func (l Layout) EmptyDirs() []string {
	return []string{
		l.Convention(),
		l.Task(),
		l.Topic(),
		l.Asset(),
		l.DocTemplate(),
		l.Archive(),
		l.Resource(),
		l.Handoff(),
		l.Lease(),
		l.Occupy(),
		l.State(),
		l.Env(),
		l.Cache(),
		l.Config(),
		l.Secrets(),
		l.Jobs(),
		l.Worktrees(),
	}
}

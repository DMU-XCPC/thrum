// Package status prints the derived picture: the board, the working directories, and
// the repositories this site has.
package status

import (
	"os"

	"github.com/multiagentworkspace/thrum/tool/internal/actors"
	"github.com/multiagentworkspace/thrum/tool/internal/jobs"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/repos"
	"github.com/multiagentworkspace/thrum/tool/internal/worktrees"
)

// Repo is one declaration and whether it is materialised here.
type Repo struct {
	Name         string `json:"name"`
	Materialised bool   `json:"materialised"`
}

// Report is everything a reading command prints.
type Report struct {
	Root      string            `json:"root"`
	Site      string            `json:"site"`
	Jobs      []jobs.Board      `json:"jobs"`
	Worktrees []worktrees.Entry `json:"worktrees"`
	Repos     []Repo            `json:"repos"`
}

// Build assembles the picture. It reads only.
func Build(l layout.Layout) (Report, error) {
	r := Report{Root: l.Root, Site: actors.Site(l)}

	boards, err := jobs.List(l)
	if err != nil {
		return r, err
	}
	r.Jobs = boards

	labels, err := worktrees.List(l)
	if err != nil {
		return r, err
	}
	r.Worktrees = labels

	decls, err := repos.List(l)
	if err != nil {
		return r, err
	}
	for _, d := range decls {
		_, statErr := os.Stat(l.CodeRepo(d.Repo))
		r.Repos = append(r.Repos, Repo{Name: d.Repo, Materialised: statErr == nil})
	}
	return r, nil
}

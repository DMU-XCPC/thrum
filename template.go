// Package thrum holds the files a workspace is created from.
//
// This file sits at the repository root rather than under tool/ because go:embed
// cannot name a path outside the directory its own source file lives in, and cannot
// contain ".." at all.
package thrum

import "embed"

// Template is the workspace template, embedded so that creating a workspace needs
// nothing on disk beside the binary. The layout of this repository and the layout of
// a workspace are independent; this is where the two touch.
//
// The all: prefix is required: without it embed skips files whose names begin with a
// dot, and the ignore rules are one of those.
//
//go:embed all:workspace-template
var Template embed.FS

// TemplateDir is the directory inside Template holding the template.
const TemplateDir = "workspace-template"

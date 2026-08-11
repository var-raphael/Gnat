// Package web embeds the dashboard's static assets and templates
// directly into the compiled binary using go:embed.
//
// Without this, every http.ServeFile call in main.go resolves its path
// relative to the process's current working directory, not relative to
// the binary itself. That works fine in local development, where gnat
// is always run from the repo root via `go run ./cmd/gnat`, but it
// breaks completely for anyone who downloads a release archive and
// runs the gnat binary from wherever they extracted it: there is no
// web/ directory sitting next to it, so every dashboard asset 404s.
// embed.FS bakes the files into the binary at build time instead, so
// the binary is truly standalone, matching what the README promises:
// download, set a few environment variables, run it, no separate
// folder of assets required alongside it.
package web

import "embed"

//go:embed static templates
var Files embed.FS

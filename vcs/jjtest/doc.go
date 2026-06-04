// Package jjtest provides fixture helpers for integration tests that exercise
// jj-mode spr behavior against a real `jj` binary inside a temporary
// colocated git+jj repository.
//
// Each call to [NewRepo] creates an isolated repo in t.TempDir() and returns
// a Repo handle exposing real JjCmd / GitCmd / VCSOperations plus convenience
// helpers for building stack topologies (commits, edits, forks).
//
// # Requirements
//
// Integration tests built on this package require the `jj` binary to be
// available on PATH. The recommended way to gate these tests is via a
// `//go:build integration` build tag, plus a TestMain that fails fast if jj
// is missing.
//
// Set SPR_SKIP_JJ_INTEGRATION=1 to skip integration tests cleanly (used by
// CI configurations that explicitly opt out, or by local developers who
// haven't installed jj).
package jjtest

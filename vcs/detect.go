package vcs

import (
	"os"
	"path/filepath"
)

// IsJJColocated returns true if the repository at rootDir is a jj-colocated
// repo — i.e. has both .jj/ and .git/ directories. spr always pushes via
// git, so jj-without-git isn't a supported configuration.
//
// Empty rootDir returns false to avoid environment-dependent results from
// filepath.Join("", ...) producing a relative path.
func IsJJColocated(rootDir string) bool {
	if rootDir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".jj")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".git")); err != nil {
		return false
	}
	return true
}

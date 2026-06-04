package vcs

import (
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeGitTestConfig() *config.Config {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeMethod = "rebase"
	return cfg
}

func TestGitOpsFetchAndRebase(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	gitmock.ExpectFetch() // expects git fetch + git rebase origin/master --autostash

	err := ops.FetchAndRebase(cfg, nil)
	require.NoError(t, err)
	gitmock.ExpectationsMet()
}

func TestGitOpsFetchAndRebase_ForceTags(t *testing.T) {
	cfg := makeGitTestConfig()
	cfg.Repo.ForceFetchTags = true
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// ExpectFetch expects "git fetch" but with force tags it's "git fetch --tags --force"
	// We need a custom expectation
	gitmock.ExpectFetchTags()

	err := ops.FetchAndRebase(cfg, nil)
	require.NoError(t, err)
	gitmock.ExpectationsMet()
}

func TestGitOpsPrepareForPush_Clean(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	gitmock.ExpectStatus() // returns empty (clean)

	cleanup, err := ops.PrepareForPush()
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	cleanup() // should not panic, no stash pop expected
	gitmock.ExpectationsMet()
}

func TestGitOpsIsEditing_NoStateFile(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	assert.False(t, ops.IsEditing())
}

func TestGitOpsEditStatePath(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// mockgit.RootDir() returns ""
	assert.Contains(t, ops.EditStatePath(), "spr_edit_state")
}

// --- CheckStackCompleteness ---

func TestGitOpsCheckStackCompleteness_Noop(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// Git mode is a no-op — detached HEAD is caught by fetchAndGetGitHubInfo
	warning := ops.CheckStackCompleteness()
	assert.Equal(t, "", warning)
	gitmock.ExpectationsMet()
}

// --- AbandonChangeIDs (no-op in git mode) ---

func TestGitOpsAbandonChangeIDs_Noop(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// Git mode has no native "abandon" operation; the method exists only
	// to satisfy the interface and must not issue git commands.
	require.NoError(t, ops.AbandonChangeIDs(nil))
	require.NoError(t, ops.AbandonChangeIDs([]string{"would-be-orphan-1", "would-be-orphan-2"}))
	gitmock.ExpectationsMet()
}

// FetchAndRebase ignores orphan IDs in git mode — no jj-style abandon path.
// This test pins that behavior so a future refactor doesn't accidentally
// start issuing git commands on the basis of orphan IDs.
func TestGitOpsFetchAndRebase_IgnoresOrphans(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	gitmock.ExpectFetch()

	err := ops.FetchAndRebase(cfg, []string{"orphan1", "orphan2"})
	require.NoError(t, err)
	gitmock.ExpectationsMet()
}

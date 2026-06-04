//go:build integration

package gittest

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain enforces git availability for this integration suite. Mirrors
// vcs/jjtest's pattern, but on git: this package contains no jj at all.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git binary not found on PATH; cannot run gittest integration suite.")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestGitMode_CascadeOrphans_FetchAndRebase reproduces the polis cascade-
// orphan scenario in git mode (no jj involved) and observes how plain
// `git rebase origin/master --autostash` — the exact call made by
// GitOps.FetchAndRebase — handles local commits whose content has been
// absorbed into an upstream squash.
//
// Setup:
//
//	origin/master = M0  (initial)
//	local         = M0 → A → B → C → D       (4 PRs stacked locally)
//
// Simulated GitHub mid-stack squash-merge: spr merges PR-C, which on
// GitHub squashes the cumulative tree A+B+C into one commit S. Origin
// becomes M0 → S. PR-D is still open and has NOT been merged.
//
//	origin/master = M0 → S                   (S contains file_a/b/c content)
//	local         = M0 → A → B → C → D
//
// We then run FetchAndRebase (= `git fetch && git rebase origin/master
// --autostash`) and inspect the result.
//
// Observed behavior (asserted below): plain `git rebase` does patch-id
// detection by default. Since A, B, C have identical patches to the ones
// already inside S, they are silently dropped. D — whose patch is NOT in
// S — survives as the only commit between origin/master and HEAD. No
// conflicts, no leftover empty commits, exit 0. Git mode self-heals.
func TestGitMode_CascadeOrphans_FetchAndRebase(t *testing.T) {
	repo := NewRepo(t)

	// Build local stack: A → B → C → D, each adding a distinct file.
	a := repo.AddCommit(t, "A: add file_a", "file_a.txt", "a-content\n")
	b := repo.AddCommit(t, "B: add file_b", "file_b.txt", "b-content\n")
	c := repo.AddCommit(t, "C: add file_c", "file_c.txt", "c-content\n")
	d := repo.AddCommit(t, "D: add file_d", "file_d.txt", "d-content\n")
	t.Logf("local stack built: A=%s B=%s C=%s D=%s", a, b, c, d)
	t.Logf("local log before fetch:\n%s", repo.Git(t, "log", "--oneline", "-5"))

	// Simulate GitHub mid-stack squash: push a single commit S that contains
	// the cumulative content of A+B+C (same bytes as our local A, B, C combined).
	s := repo.PushSquashToOrigin(t, "S: squash of A+B+C (PR-C merged on GitHub)", map[string]string{
		"file_a.txt": "a-content\n",
		"file_b.txt": "b-content\n",
		"file_c.txt": "c-content\n",
	})
	t.Logf("origin/master advanced to S=%s (contains A+B+C content)", s)

	// Run what `spr update` does in git mode.
	err := repo.GitOps.FetchAndRebase(repo.Cfg)

	t.Logf("FetchAndRebase returned err=%v", err)

	// Observe the result regardless of outcome.
	logOneline := repo.Git(t, "log", "--oneline", "-10")
	t.Logf("git log --oneline -10:\n%s", logOneline)

	status := repo.Git(t, "status", "--porcelain=v1", "--branch")
	t.Logf("git status:\n%s", status)

	// If we're in the middle of a conflicted rebase, surface what's stuck.
	if _, statErr := os.Stat(repo.Path + "/.git/rebase-merge"); statErr == nil {
		t.Logf(".git/rebase-merge exists — rebase is paused (likely conflict)")
		if data, readErr := os.ReadFile(repo.Path + "/.git/rebase-merge/done"); readErr == nil {
			t.Logf("rebase-merge/done:\n%s", string(data))
		}
		if data, readErr := os.ReadFile(repo.Path + "/.git/rebase-merge/git-rebase-todo"); readErr == nil {
			t.Logf("rebase-merge/git-rebase-todo:\n%s", string(data))
		}
	}
	if _, statErr := os.Stat(repo.Path + "/.git/rebase-apply"); statErr == nil {
		t.Logf(".git/rebase-apply exists — am-style rebase is paused")
	}

	// Reachability of the orphan commits A/B/C and the un-merged D in the
	// post-rebase HEAD lineage. Each is checked individually so we get a
	// clear picture (not just "all" or "none").
	headLineage := repo.Git(t, "log", "--format=%H", "HEAD")
	containsCommit := func(short string) bool {
		long := strings.TrimSpace(repo.tryGit(t, "rev-parse", short))
		if long == "" {
			return false
		}
		return strings.Contains(headLineage, long)
	}
	t.Logf("HEAD lineage contains A? %v  B? %v  C? %v  D? %v",
		containsCommit(a), containsCommit(b), containsCommit(c), containsCommit(d))

	// Git's own view of the diff vs origin/master post-rebase.
	commitsAheadOneline := repo.Git(t, "log", "origin/master..HEAD", "--oneline")
	t.Logf("git log origin/master..HEAD --oneline:\n%s", commitsAheadOneline)
	diffStat := repo.Git(t, "diff", "origin/master..HEAD", "--stat")
	t.Logf("git diff origin/master..HEAD --stat:\n%s", diffStat)

	// Assertions — locking in the observed self-healing behavior so we
	// catch any regression if git's default changes or our FetchAndRebase
	// adds flags that defeat patch-id detection.
	require.NoError(t, err, "FetchAndRebase should succeed: orphan patches should be dropped via patch-id")

	// Working copy clean (no in-progress rebase, no merge state).
	assert.Equal(t, "## master", strings.TrimSpace(status),
		"working copy should be clean after rebase")

	// Exactly one commit ahead of origin/master: the rebased D.
	commitsAhead := strings.Split(strings.TrimSpace(commitsAheadOneline), "\n")
	if commitsAheadOneline == "" {
		commitsAhead = nil
	}
	assert.Len(t, commitsAhead, 1,
		"exactly one commit should remain ahead of origin/master (the rebased D); got %d", len(commitsAhead))
	if len(commitsAhead) == 1 {
		assert.Contains(t, commitsAhead[0], "D: add file_d",
			"the surviving commit should be D")
	}

	// Diff against origin/master should be exactly file_d.txt — A/B/C's
	// content is already in S and their commits are gone.
	assert.Contains(t, diffStat, "file_d.txt",
		"D's content should be in the diff against origin/master")
	assert.NotContains(t, diffStat, "file_a.txt",
		"A's content should be absorbed by S, not appear as ahead-diff")
	assert.NotContains(t, diffStat, "file_b.txt",
		"B's content should be absorbed by S")
	assert.NotContains(t, diffStat, "file_c.txt",
		"C's content should be absorbed by S")
}

// tryGit is like Repo.Git but returns the stdout (or stderr message) on
// error instead of failing the test, so we can include diagnostic output
// even when individual queries fail (e.g. mid-rebase rev-parse).
func (r Repo) tryGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(r.Path, "git", args...)
	if err != nil {
		return fmt.Sprintf("(err: %v) %s", err, out)
	}
	return strings.TrimRight(out, "\n")
}

//go:build integration

package vcs_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/vcs/jjtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain enforces the jj-availability contract documented in vcs/jjtest:
// fail fast unless SPR_SKIP_JJ_INTEGRATION=1 is set, in which case skip the
// whole package cleanly.
func TestMain(m *testing.M) {
	if os.Getenv("SPR_SKIP_JJ_INTEGRATION") == "1" {
		fmt.Fprintln(os.Stderr, "SPR_SKIP_JJ_INTEGRATION=1 set, skipping jj integration tests")
		os.Exit(0)
	}
	if _, err := exec.LookPath("jj"); err != nil {
		fmt.Fprintln(os.Stderr,
			"jj binary not found on PATH. Install jj (https://jj-vcs.github.io/jj/) "+
				"or set SPR_SKIP_JJ_INTEGRATION=1 to skip integration tests.")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestJjIntegration_Smoke creates a colocated repo and exercises the most
// basic path: @ exists, GetLocalCommitStack returns empty for a fresh repo,
// and op-log snapshot/diff round-trips cleanly.
func TestJjIntegration_Smoke(t *testing.T) {
	repo := jjtest.NewRepo(t)

	at := repo.At(t)
	require.NotEmpty(t, at, "fresh repo should have an @ change id")

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Empty(t, commits, "fresh repo (no commits above trunk) should have empty stack")

	snapshot := repo.SnapshotOpLog(t)
	repo.AssertOpsSince(t, snapshot) // nothing happened since snapshot
}

// --- Auto-revset (GetLocalCommitStack) ---
//
// The connected-component revset `trunk()..(@:: | ::@)` means the stack is
// returned in full regardless of where @ sits within it.

func TestJjIntegration_GetStack_AtTop(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "bottom", true)
	c2 := repo.AddCommit(t, "middle", true)
	c3 := repo.AddCommit(t, "top", true)
	// @ is on a new empty commit on top of c3 after AddCommit; move @ to c3.
	repo.Edit(t, c3)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3)
	require.Equal(t, c1, commits[0].ChangeID)
	require.Equal(t, c2, commits[1].ChangeID)
	require.Equal(t, c3, commits[2].ChangeID)
}

func TestJjIntegration_GetStack_AtMiddle(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "bottom", true)
	c2 := repo.AddCommit(t, "middle", true)
	c3 := repo.AddCommit(t, "top", true)

	// Move @ to the middle — the bug the auto-revset fixed was that the
	// old revset trunk()..@ would silently drop c3 in this case.
	repo.Edit(t, c2)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3, "auto-revset must return the whole stack even when @ is mid-stack")
	require.Equal(t, c1, commits[0].ChangeID)
	require.Equal(t, c2, commits[1].ChangeID)
	require.Equal(t, c3, commits[2].ChangeID)
}

func TestJjIntegration_GetStack_AtBottom(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "bottom", true)
	c2 := repo.AddCommit(t, "middle", true)
	c3 := repo.AddCommit(t, "top", true)
	repo.Edit(t, c1)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3)
	require.Equal(t, c1, commits[0].ChangeID)
	require.Equal(t, c2, commits[1].ChangeID)
	require.Equal(t, c3, commits[2].ChangeID)
}

func TestJjIntegration_GetStack_OnTrunk_NoStack(t *testing.T) {
	repo := jjtest.NewRepo(t)
	// Fresh repo, @ is on an empty commit immediately above trunk.
	// trunk()..(@:: | ::@) is empty (or contains only @ which is empty).
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Empty(t, commits)
}

func TestJjIntegration_GetStack_DivergentSibling_Excluded(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "stack A bottom", true)
	c2 := repo.AddCommit(t, "stack A top", true)
	// Build a disjoint sibling stack rooted on trunk (parent = "root()+")
	// via Fork: parent is trunk's tip.
	trunkTip := mustTrunkTip(t, repo)
	d1 := repo.Fork(t, trunkTip, "stack B bottom")
	_ = repo.AddCommit(t, "stack B top", true)

	// Back to stack A and assert we only see A.
	repo.Edit(t, c2)
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Len(t, commits, 2)
	require.Equal(t, c1, commits[0].ChangeID)
	require.Equal(t, c2, commits[1].ChangeID)
	// Sanity: d1 is NOT in the result.
	for _, c := range commits {
		require.NotEqual(t, d1, c.ChangeID, "stack B must not appear when @ is on stack A")
	}
}

func TestJjIntegration_GetStack_WIPTruncates(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	wip := repo.AddCommit(t, "WIP in progress", true)
	c3 := repo.AddCommit(t, "C3", true)
	_ = c3

	repo.Edit(t, wip)
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3, "WIP should still appear in the stack — truncation happens in spr layer")
	require.Equal(t, c1, commits[0].ChangeID)
	require.True(t, commits[1].WIP, "WIP commit must be flagged")
	require.False(t, commits[0].WIP)
	require.False(t, commits[2].WIP)
}

func TestJjIntegration_GetStack_NewMidStackCommit_AutoAddsTrailer(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)

	// Insert a commit ABOVE c1 (between c1 and c2) WITHOUT a commit-id
	// trailer. spr's GetLocalCommitStack should detect the missing trailer
	// and auto-add it via `jj describe`.
	repo.Edit(t, c1)
	inserted := repo.InsertAbove(t, "inserted with no trailer", false)
	// Rebase c2 onto the inserted commit so the stack is linear:
	// c1 → inserted → c2
	mustJjCmd(t, repo, "rebase", "-s", c2, "-d", inserted)

	repo.Edit(t, c2) // top of the stack
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Len(t, commits, 3)
	for _, c := range commits {
		require.NotEmpty(t, c.CommitID,
			"GetLocalCommitStack must auto-add commit-id trailers via jj describe")
	}
}

// --- WIP positioning ---
//
// WIP markers are spr's opt-out mechanism: any commit whose subject starts
// with "WIP" is returned in the stack but flagged so the spr update loop
// breaks at it. These tests pin that the VCS layer returns WIP commits as
// part of the stack (truncation is the spr layer's job).

func TestJjIntegration_GetStack_WIPAtTop(t *testing.T) {
	repo := jjtest.NewRepo(t)
	repo.AddCommit(t, "C1", true)
	repo.AddCommit(t, "C2", true)
	wip := repo.AddCommit(t, "WIP in progress", true)
	repo.Edit(t, wip)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3, "WIP at top must NOT truncate the VCS-layer return")
	require.False(t, commits[0].WIP)
	require.False(t, commits[1].WIP)
	require.True(t, commits[2].WIP, "WIP flag must be set on the WIP commit")
}

func TestJjIntegration_GetStack_MultipleConsecutiveWIPs(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	repo.AddCommit(t, "WIP first", true)
	wip2 := repo.AddCommit(t, "WIP second", true)
	repo.Edit(t, wip2)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)

	require.Len(t, commits, 3)
	assert.Equal(t, c1, commits[0].ChangeID)
	assert.False(t, commits[0].WIP)
	assert.True(t, commits[1].WIP, "both WIPs flagged")
	assert.True(t, commits[2].WIP)
}

// --- Multi-head detection (CheckStackCompleteness) ---

func TestJjIntegration_Check_LinearStack(t *testing.T) {
	repo := jjtest.NewRepo(t)
	repo.AddCommit(t, "C1", true)
	repo.AddCommit(t, "C2", true)
	repo.AddCommit(t, "C3", true)

	warning := repo.JjOps.CheckStackCompleteness()
	require.Empty(t, warning, "linear stack must not produce a warning")
}

func TestJjIntegration_Check_EmptyStack(t *testing.T) {
	repo := jjtest.NewRepo(t)
	warning := repo.JjOps.CheckStackCompleteness()
	require.Empty(t, warning)
}

func TestJjIntegration_Check_TwoHeadsFork(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	_ = repo.AddCommit(t, "C2", true)
	// Fork off c1: two heads above trunk (c2 on main chain, sibling).
	repo.Fork(t, c1, "experimental sibling")
	// Move @ to c1 — the branching point — so both heads (c2 and sibling)
	// are descendants of @ and thus inside the connected component
	// `trunk()..(@:: | ::@)`.
	repo.Edit(t, c1)

	warning := repo.JjOps.CheckStackCompleteness()
	require.NotEmpty(t, warning, "multi-head fork must produce a warning when @ is at or below the branching point")
	require.Contains(t, warning, "non-linear")
}

// TestJjIntegration_Check_ForkAbove_OnMainStack documents the *limitation* of
// the current detection: when @ is on the main chain above the branching
// point, sibling stacks are outside `(@:: | ::@)` and the check returns
// empty. This is intentional — those siblings aren't part of "your stack"
// from @'s viewpoint — but worth pinning so a future change doesn't quietly
// flip this behavior.
func TestJjIntegration_Check_ForkAbove_OnMainStack(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)
	repo.Fork(t, c1, "experimental sibling")
	repo.Edit(t, c2) // back on the main chain, above the branching point

	warning := repo.JjOps.CheckStackCompleteness()
	require.Empty(t, warning, "siblings outside the connected component must not trigger multi-head")
}

// --- EditStart ---

func TestJjIntegration_EditStart_MovesAt(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)
	_ = c2
	// @ is at the top. Editing c1 should move @ there.
	commit := gitCommitForChange(c1, "C1")
	err := repo.JjOps.EditStart(commit)
	require.NoError(t, err)
	require.Equal(t, c1, repo.At(t), "@ must be on c1 after EditStart")
}

func TestJjIntegration_EditStart_NoChangeID_Error(t *testing.T) {
	repo := jjtest.NewRepo(t)
	before := repo.At(t)
	err := repo.JjOps.EditStart(gitCommitForChange("", "no change id"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no jj change ID")
	require.Equal(t, before, repo.At(t), "@ unchanged on error")
}

func TestJjIntegration_EditStart_ImmutableCommit_Error(t *testing.T) {
	repo := jjtest.NewRepo(t)
	// trunk()'s tip is the master@origin commit — immutable by default.
	trunkTip := mustTrunkTip(t, repo)
	err := repo.JjOps.EditStart(gitCommitForChange(trunkTip, "trunk"))
	require.Error(t, err, "editing an immutable commit must error out")
}

// --- Fetch ---

func TestJjIntegration_Fetch_Succeeds(t *testing.T) {
	repo := jjtest.NewRepo(t)
	require.NoError(t, repo.JjOps.Fetch())
}

// --- FetchAndRebase ---

func TestJjIntegration_FetchAndRebase_NoRebase(t *testing.T) {
	repo := jjtest.NewRepo(t)
	repo.Cfg.User.NoRebase = true
	require.NoError(t, repo.JjOps.FetchAndRebase(repo.Cfg, nil))
}

func TestJjIntegration_FetchAndRebase_Rebases(t *testing.T) {
	repo := jjtest.NewRepo(t)
	repo.AddCommit(t, "C1", true)
	require.NoError(t, repo.JjOps.FetchAndRebase(repo.Cfg, nil))
}

// --- AmendInto ---

func TestJjIntegration_AmendInto_SquashesWorkingCopy(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)
	_ = c2
	// Working copy gets some uncommitted changes (a new file).
	mustWriteFile(t, repo, "newfile.txt", "amend payload")
	// Amend into c1.
	err := repo.JjOps.AmendInto(gitCommitForChange(c1, "C1"))
	require.NoError(t, err)
	// c1's change id should still exist (jj squash preserves it).
	require.NotEmpty(t, mustJjCmd(t, repo, "log", "--no-graph", "-r", c1, "-T", "change_id"))
}

// --- PushBranches ---

func TestJjIntegration_PushBranches_SetsBookmarksAndPushes(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)

	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Len(t, commits, 2)

	require.NoError(t, repo.JjOps.PushBranches(repo.Cfg, commits, false))

	// Bookmarks present locally.
	bookmarks := mustJjCmd(t, repo, "bookmark", "list", "-T", `name ++ "\n"`)
	require.Contains(t, bookmarks, "spr/master/"+commits[0].CommitID)
	require.Contains(t, bookmarks, "spr/master/"+commits[1].CommitID)

	// Bookmarks present on the bare origin.
	originRefs := mustRunIn(t, repo.RemotePath, "git", "branch", "--list")
	require.Contains(t, originRefs, "spr/master/"+commits[0].CommitID)
	require.Contains(t, originRefs, "spr/master/"+commits[1].CommitID)
	_ = c1
	_ = c2
}

// TestJjIntegration_PushBranches_ConflictedCommit_Refused pins the key
// safety property we lean on in the design: jj's `git push` refuses to push
// commits containing conflict markers, so spr's PushBranches will fail
// loudly instead of pushing broken commits to GitHub.
//
// We construct a conflict by editing the bottom of a 2-commit stack such
// that the top no longer rebases cleanly.
func TestJjIntegration_PushBranches_ConflictedCommit_Refused(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1 base", true)
	c2 := repo.AddCommit(t, "C2 dependent", true)

	// Both commits touched their own file_<token>.txt — disjoint, can't
	// conflict. Force a real conflict by editing the same file in both.
	// Inject a shared file into c1, then a conflicting version into c2.
	repo.Edit(t, c1)
	mustWriteFile(t, repo, "shared.txt", "from c1 first\n")
	repo.Edit(t, c2)
	mustWriteFile(t, repo, "shared.txt", "from c2 second\n")

	// Now go back to c1 and rewrite the shared file to a third version.
	// jj auto-rebases c2 → its version of shared.txt vs the new c1 base
	// produces a conflict.
	repo.Edit(t, c1)
	mustWriteFile(t, repo, "shared.txt", "from c1 REWRITTEN\n")

	// Re-fetch the stack now that contents have changed.
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Len(t, commits, 2)

	err := repo.JjOps.PushBranches(repo.Cfg, commits, false)
	require.Error(t, err, "jj git push must refuse conflicted commits")
	assert.True(t,
		strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "conflicts"),
		"error must mention conflicts, got: %s", err.Error())
}

// --- Real JjCmd error paths (Jj, JjArgs, MustJj) ---
//
// These exercise the real jj binary against deliberately invalid inputs
// to pin error reporting. Unit tests use mockjj and don't run the real
// JjCmd code at all, so these are the only place these branches get
// covered.

func TestJjCmdJj_ErrorIncludesStdoutAndStderr(t *testing.T) {
	repo := jjtest.NewRepo(t)
	// Use a bogus subcommand so real jj fails. jj writes its error to
	// stderr; our fix in jj_cmd.go captures both streams separately and
	// includes them in the wrapped error message.
	var out string
	err := repo.JjCmd.Jj("this-is-not-a-real-subcommand", &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stderr:", "wrapped error must include stderr context")
}

func TestJjCmdJjArgs_ErrorIncludesStdoutAndStderr(t *testing.T) {
	repo := jjtest.NewRepo(t)
	var out string
	err := repo.JjCmd.JjArgs(
		[]string{"log", "-r", "this-is-not-a-real-revset@@@@"}, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stderr:")
}

func TestJjCmdMustJj_PanicsOnError(t *testing.T) {
	repo := jjtest.NewRepo(t)
	// MustJj wraps Jj; the only branch that needs coverage is the
	// panic-on-error path.
	require.Panics(t, func() {
		repo.JjCmd.MustJj("this-is-not-a-real-subcommand", nil)
	})
}

func TestJjCmdMustJj_NoPanicOnSuccess(t *testing.T) {
	repo := jjtest.NewRepo(t)
	// Pin the happy path: MustJj on a valid command must not panic.
	require.NotPanics(t, func() {
		var out string
		repo.JjCmd.MustJj("op log --no-graph -n 1 -T description", &out)
	})
}

// --- helpers used only by integration tests ---

func gitCommitForChange(changeID, subject string) git.Commit {
	return git.Commit{
		ChangeID:   changeID,
		CommitID:   "deadbeef",
		CommitHash: "0000000000000000000000000000000000000000",
		Subject:    subject,
	}
}

func mustTrunkTip(t *testing.T, repo jjtest.Repo) string {
	t.Helper()
	cmd := exec.Command("jj", "log", "--no-graph", "-r", "trunk()", "-T", "change_id")
	cmd.Dir = repo.Path
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "trunk(): %s", string(out))
	id := strings.TrimSpace(string(out))
	require.NotEmpty(t, id, "trunk() must resolve to a change")
	return id
}

func mustJjCmd(t *testing.T, repo jjtest.Repo, args ...string) string {
	t.Helper()
	cmd := exec.Command("jj", args...)
	cmd.Dir = repo.Path
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "jj %s: %s", strings.Join(args, " "), string(out))
	return string(out)
}

func mustRunIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s %s: %s", name, strings.Join(args, " "), string(out))
	return string(out)
}

func mustWriteFile(t *testing.T, repo jjtest.Repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo.Path, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

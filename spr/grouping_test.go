package spr

import (
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupCommitsIntoPRs_BasicGrouping(t *testing.T) {
	// 5 commits, branches at c2 and c4 → 2 groups
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1"},
		{CommitID: "00000002", Subject: "c2", Branches: []string{"feature-a"}},
		{CommitID: "00000003", Subject: "c3"},
		{CommitID: "00000004", Subject: "c4", Branches: []string{"feature-b"}},
		{CommitID: "00000005", Subject: "c5"},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	require.Len(t, groups, 2)
	assert.Equal(t, "feature-a", groups[0].LocalBranch)
	assert.Len(t, groups[0].Commits, 2)
	assert.Equal(t, "00000001", groups[0].Commits[0].CommitID)
	assert.Equal(t, "00000002", groups[0].Commits[1].CommitID)
	assert.Equal(t, "00000002", groups[0].TipCommit().CommitID)

	assert.Equal(t, "feature-b", groups[1].LocalBranch)
	assert.Len(t, groups[1].Commits, 2)
	assert.Equal(t, "00000003", groups[1].Commits[0].CommitID)
	assert.Equal(t, "00000004", groups[1].Commits[1].CommitID)

	// c5 is WIP (above last branch)
	require.Len(t, wip, 1)
	assert.Equal(t, "00000005", wip[0].CommitID)
}

func TestGroupCommitsIntoPRs_NoBranches(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1"},
		{CommitID: "00000002", Subject: "c2"},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	assert.Len(t, groups, 0)
	assert.Len(t, wip, 2)
}

func TestGroupCommitsIntoPRs_SingleBranchAtTop(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1"},
		{CommitID: "00000002", Subject: "c2"},
		{CommitID: "00000003", Subject: "c3", Branches: []string{"my-feature"}},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	require.Len(t, groups, 1)
	assert.Equal(t, "my-feature", groups[0].LocalBranch)
	assert.Len(t, groups[0].Commits, 3)
	assert.Len(t, wip, 0)
}

func TestGroupCommitsIntoPRs_FiltersSPRBranches(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1", Branches: []string{"spr/main/abc12345"}},
		{CommitID: "00000002", Subject: "c2", Branches: []string{"real-branch"}},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	require.Len(t, groups, 1)
	assert.Equal(t, "real-branch", groups[0].LocalBranch)
	assert.Len(t, groups[0].Commits, 2)
	assert.Len(t, wip, 0)
}

func TestGroupCommitsIntoPRs_FiltersTargetBranch(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1", Branches: []string{"main"}},
		{CommitID: "00000002", Subject: "c2"},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	assert.Len(t, groups, 0)
	assert.Len(t, wip, 2)
}

func TestGroupCommitsIntoPRs_MultipleBranchesOnSameCommit(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1", Branches: []string{"branch-a", "branch-b"}},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	require.Len(t, groups, 1)
	assert.Equal(t, "branch-a", groups[0].LocalBranch) // uses first
	assert.Len(t, wip, 0)
}

func TestGroupCommitsIntoPRs_WIPAtTop(t *testing.T) {
	commits := []git.Commit{
		{CommitID: "00000001", Subject: "c1", Branches: []string{"feature"}},
		{CommitID: "00000002", Subject: "WIP c2"},
		{CommitID: "00000003", Subject: "WIP c3"},
	}

	groups, wip := GroupCommitsIntoPRs(commits, "main")

	require.Len(t, groups, 1)
	assert.Equal(t, "feature", groups[0].LocalBranch)
	assert.Len(t, wip, 2)
}

func TestGroupCommitsIntoPRs_EmptyInput(t *testing.T) {
	groups, wip := GroupCommitsIntoPRs(nil, "main")
	assert.Len(t, groups, 0)
	assert.Len(t, wip, 0)
}

func TestTipCommits(t *testing.T) {
	groups := []PRGroup{
		{
			LocalBranch: "a",
			Commits: []git.Commit{
				{CommitID: "00000001"},
				{CommitID: "00000002"},
			},
		},
		{
			LocalBranch: "b",
			Commits: []git.Commit{
				{CommitID: "00000003"},
			},
		},
	}

	tips := TipCommits(groups)
	require.Len(t, tips, 2)
	assert.Equal(t, "00000002", tips[0].CommitID)
	assert.Equal(t, "00000003", tips[1].CommitID)
}

func TestBuildGroupMap(t *testing.T) {
	groups := []PRGroup{
		{
			LocalBranch: "a",
			Commits: []git.Commit{
				{CommitID: "00000001"},
				{CommitID: "00000002"},
			},
		},
	}

	gm := BuildGroupMap(groups)
	require.Len(t, gm, 1)
	commits, ok := gm["00000002"]
	require.True(t, ok)
	assert.Len(t, commits, 2)
}

func TestPRGroup_IsWIP(t *testing.T) {
	group := PRGroup{
		Commits: []git.Commit{
			{CommitID: "00000001", Subject: "normal"},
			{CommitID: "00000002", Subject: "WIP stuff", WIP: true},
		},
	}
	assert.True(t, group.IsWIP())

	group2 := PRGroup{
		Commits: []git.Commit{
			{CommitID: "00000001", Subject: "normal"},
		},
	}
	assert.False(t, group2.IsWIP())
}

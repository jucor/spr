package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSPRBranch(t *testing.T) {
	assert.True(t, IsSPRBranch("spr/main/deadbeef"))
	assert.True(t, IsSPRBranch("spr/master/abc12345"))
	assert.False(t, IsSPRBranch("my-feature"))
	assert.False(t, IsSPRBranch("main"))
	assert.False(t, IsSPRBranch("spr-related"))
}

func TestAnnotateCommitsWithBranches(t *testing.T) {
	commits := []Commit{
		{CommitHash: "hash1", CommitID: "00000001"},
		{CommitHash: "hash2", CommitID: "00000002"},
		{CommitHash: "hash3", CommitID: "00000003"},
	}

	branchMap := map[string][]string{
		"hash1": {"feature-a", "spr/main/abc12345"},
		"hash2": {"main"},
		"hash3": {"feature-b"},
	}

	AnnotateCommitsWithBranches(commits, branchMap, "main")

	// feature-a kept, spr branch filtered
	assert.Equal(t, []string{"feature-a"}, commits[0].Branches)
	// main filtered (target branch)
	assert.Len(t, commits[1].Branches, 0)
	// feature-b kept
	assert.Equal(t, []string{"feature-b"}, commits[2].Branches)
}

func TestAnnotateCommitsWithBranches_NilMap(t *testing.T) {
	commits := []Commit{{CommitHash: "hash1", CommitID: "00000001"}}
	AnnotateCommitsWithBranches(commits, nil, "main")
	assert.Nil(t, commits[0].Branches)
}

func TestBranchNameRegex(t *testing.T) {
	tests := []struct {
		input  string
		branch string
		commit string
	}{
		{input: "spr/b1/deadbeef", branch: "b1", commit: "deadbeef"},
	}

	for _, tc := range tests {
		matches := BranchNameRegex.FindStringSubmatch(tc.input)
		if tc.branch != matches[1] {
			t.Fatalf("expected: '%v', actual: '%v'", tc.branch, matches[1])
		}
		if tc.commit != matches[2] {
			t.Fatalf("expected: '%v', actual: '%v'", tc.commit, matches[2])
		}
	}
}

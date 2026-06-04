package git

import (
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeGitLog formats one commit as `git log --format=medium` would,
// optionally with a commit-id trailer. Helper for the parse tests below.
func makeGitLog(hash, subject, commitID string) string {
	body := ""
	if commitID != "" {
		body = "\n    commit-id: " + commitID + "\n"
	}
	return "commit " + hash + "\nAuthor: Test <t@example.com>\nDate: Mon Jan 1 00:00:00 1970 +0000\n\n    " + subject + "\n" + body + "\n"
}

func TestParseLocalCommitStack_SingleValid(t *testing.T) {
	in := makeGitLog("c100000000000000000000000000000000000000", "feature 1", "00000001")
	commits, valid := parseLocalCommitStack(in)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "00000001", commits[0].CommitID)
	assert.Equal(t, "c100000000000000000000000000000000000000", commits[0].CommitHash)
	assert.Equal(t, "feature 1", commits[0].Subject)
}

func TestParseLocalCommitStack_MissingTrailer_InvalidLast(t *testing.T) {
	// Last commit in the log (= bottom of stack) lacks a commit-id.
	// Parser sets commitScanOn=true on hash line; without a closing
	// commit-id line, the loop ends with commitScanOn still true, which
	// triggers the "missing last commit id" branch.
	in := "commit c100000000000000000000000000000000000000\nAuthor: T <t@e>\nDate: x\n\n    subject\n"
	commits, valid := parseLocalCommitStack(in)
	require.False(t, valid, "trailing commit without commit-id must return valid=false")
	require.Empty(t, commits, "no commit emitted when invalid")
}

func TestParseLocalCommitStack_MissingTrailer_InvalidMiddle(t *testing.T) {
	// Two commits, first one (top in git log order) lacks a commit-id.
	// Parser sees a new "commit <hash>" line while commitScanOn is still
	// true → returns nil, false ("missing the commit-id" branch on the
	// earlier blockage).
	in := "commit c100000000000000000000000000000000000000\nAuthor: T <t@e>\nDate: x\n\n    no trailer here\n\n" +
		"commit c200000000000000000000000000000000000000\nAuthor: T <t@e>\nDate: x\n\n    has trailer\n\n    commit-id: 00000002\n"
	_, valid := parseLocalCommitStack(in)
	require.False(t, valid)
}

func TestParseLocalCommitStack_ReversesOrder(t *testing.T) {
	// `git log` is newest-first; parseLocalCommitStack returns oldest-
	// first. Verify the reversal.
	in := makeGitLog("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "top", "00000002") +
		makeGitLog("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "bottom", "00000001")
	commits, valid := parseLocalCommitStack(in)
	require.True(t, valid)
	require.Len(t, commits, 2)
	assert.Equal(t, "00000001", commits[0].CommitID, "bottom commit first")
	assert.Equal(t, "00000002", commits[1].CommitID, "top commit last")
}

func TestParseLocalCommitStack_WIPDetected(t *testing.T) {
	in := makeGitLog("c100000000000000000000000000000000000000", "WIP refactor", "00000001")
	commits, valid := parseLocalCommitStack(in)
	require.True(t, valid)
	require.True(t, commits[0].WIP, "subject starting with WIP must flag commit")
}

func TestParseLocalCommitStack_EmptyInput(t *testing.T) {
	commits, valid := parseLocalCommitStack("")
	require.True(t, valid, "empty input is valid (no commits to validate)")
	require.Empty(t, commits)
}

func TestParseLocalCommitStack_OnlyWhitespace(t *testing.T) {
	commits, valid := parseLocalCommitStack("\n\n\n")
	require.True(t, valid)
	require.Empty(t, commits)
	_ = strings.TrimSpace // silence unused import lint if helper changes
}

func TestBranchNameRegex(t *testing.T) {
	tests := []struct {
		prefix string
		input  string
		branch string
		commit string
	}{
		{prefix: "spr", input: "spr/b1/deadbeef", branch: "b1", commit: "deadbeef"},
		{prefix: "spr", input: "spr/main/abcd1234", branch: "main", commit: "abcd1234"},
		{prefix: "custom", input: "custom/main/deadbeef", branch: "main", commit: "deadbeef"},
		{prefix: "my-team", input: "my-team/develop/abcd1234", branch: "develop", commit: "abcd1234"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			matches := BranchNameRegex(tc.prefix).FindStringSubmatch(tc.input)
			assert.NotNil(t, matches)
			assert.Equal(t, tc.branch, matches[1])
			assert.Equal(t, tc.commit, matches[2])
		})
	}
}

func TestBranchNameRegexNoMatch(t *testing.T) {
	tests := []struct {
		prefix string
		input  string
	}{
		{prefix: "spr", input: "other/main/deadbeef"},
		{prefix: "custom", input: "spr/main/deadbeef"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			matches := BranchNameRegex(tc.prefix).FindStringSubmatch(tc.input)
			assert.Nil(t, matches)
		})
	}
}

func TestBranchNameFromCommit(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		branch   string
		commitID string
		expected string
	}{
		{
			name:     "default prefix",
			prefix:   "spr",
			branch:   "main",
			commitID: "deadbeef",
			expected: "spr/main/deadbeef",
		},
		{
			name:     "custom prefix",
			prefix:   "my-team",
			branch:   "develop",
			commitID: "abcd1234",
			expected: "my-team/develop/abcd1234",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.EmptyConfig()
			cfg.User.BranchPrefix = tc.prefix
			cfg.Repo.GitHubBranch = tc.branch

			commit := Commit{CommitID: tc.commitID}
			result := BranchNameFromCommit(cfg, commit)
			assert.Equal(t, tc.expected, result)
		})
	}
}

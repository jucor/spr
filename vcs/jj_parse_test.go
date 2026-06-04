package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJjLogOutput_SingleCommit(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fmychangeid1234\x1ffalse\x1ftest commit 1\n\ncommit-id:00000001\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "00000001", commits[0].sprCommitID)
	assert.Equal(t, "mychangeid1234", commits[0].changeID)
	assert.Equal(t, "c100000000000000000000000000000000000000", commits[0].commitHash)
	assert.Equal(t, "test commit 1", commits[0].subject)
	assert.False(t, commits[0].wip)
	assert.False(t, commits[0].empty)
}

func TestParseJjLogOutput_MultipleCommits(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1fcommit 1\n\ncommit-id:00000001\n\x1e" +
		"c200000000000000000000000000000000000000\x1fchange2\x1ffalse\x1fcommit 2\n\ncommit-id:00000002\n\x1e" +
		"c300000000000000000000000000000000000000\x1fchange3\x1ffalse\x1fcommit 3\n\ncommit-id:00000003\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 3)
	assert.Equal(t, "00000001", commits[0].sprCommitID)
	assert.Equal(t, "00000002", commits[1].sprCommitID)
	assert.Equal(t, "00000003", commits[2].sprCommitID)
	assert.Equal(t, "change1", commits[0].changeID)
	assert.Equal(t, "change2", commits[1].changeID)
	assert.Equal(t, "change3", commits[2].changeID)
}

func TestParseJjLogOutput_MissingCommitID(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1fcommit without trailer\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.False(t, valid) // invalid because non-empty commit lacks commit-id
	require.Len(t, commits, 1)
	assert.Equal(t, "", commits[0].sprCommitID)
	assert.Equal(t, "change1", commits[0].changeID)
	assert.Equal(t, "commit without trailer", commits[0].subject)
}

func TestParseJjLogOutput_EmptyCommitSkipped(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ftrue\x1f\x1e" +
		"c200000000000000000000000000000000000000\x1fchange2\x1ffalse\x1freal commit\n\ncommit-id:00000001\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1) // empty commit skipped
	assert.Equal(t, "change2", commits[0].changeID)
}

func TestParseJjLogOutput_WIPPrefix(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1fWIP work in progress\n\ncommit-id:00000001\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.True(t, commits[0].wip)
	assert.Equal(t, "WIP work in progress", commits[0].subject)
}

func TestParseJjLogOutput_MultiLineBody(t *testing.T) {
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1fFix the bug\n\nThis is a detailed\ndescription of the fix.\n\ncommit-id:deadbeef\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "Fix the bug", commits[0].subject)
	assert.Equal(t, "deadbeef", commits[0].sprCommitID)
	assert.Contains(t, commits[0].body, "detailed")
	assert.Contains(t, commits[0].body, "description of the fix")
}

func TestParseJjLogOutput_EmptyInput(t *testing.T) {
	commits, valid := parseJjLogOutput("")
	require.True(t, valid) // no commits = valid (nothing to check)
	require.Len(t, commits, 0)
}

func TestParseJjLogOutput_CommitIDWithSpace(t *testing.T) {
	// commit-id: with a space after colon (spr regex allows this)
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1ftest commit\n\ncommit-id: abcdef01\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "abcdef01", commits[0].sprCommitID)
}

// --- Trailer parsing edge cases ---
//
// commitIDRegex is `commit-id:\s*([a-f0-9]{8})` — case-sensitive, content-
// based (not line-based). These tests pin the resulting behavior.

func TestParseJjLogOutput_MultipleTrailers_FirstWins(t *testing.T) {
	// Two commit-id lines. FindStringSubmatch returns the first match
	// in the input, so the first one wins. Pin this so a future regex
	// change doesn't silently flip to last-wins.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\ncommit-id:aaaaaaaa\ncommit-id:bbbbbbbb\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "aaaaaaaa", commits[0].sprCommitID)
}

func TestParseJjLogOutput_TrailerWithExtraWhitespace(t *testing.T) {
	// `\s*` matches one space already; multiple spaces also fine.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\ncommit-id:   abcd1234\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	assert.Equal(t, "abcd1234", commits[0].sprCommitID)
}

func TestParseJjLogOutput_UppercaseTrailerIgnored(t *testing.T) {
	// `Commit-Id:` (capital C, capital I) is NOT recognized. The commit
	// is treated as if it had no trailer → valid=false.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\nCommit-Id:abcd1234\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.False(t, valid, "uppercase trailer must NOT match (case-sensitive regex)")
	assert.Empty(t, commits[0].sprCommitID)
}

func TestParseJjLogOutput_TrailerLikeStringInBody(t *testing.T) {
	// The regex matches anywhere in the description, including the body
	// — pinning current behavior. If the user references commit-id:abcd1234
	// in their PR body, spr will treat it as the trailer. This is a known
	// behavior, not necessarily desirable, but pinning prevents accidental
	// regression in either direction.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\n" +
		"This PR follows commit-id:fedcba98 in the related work.\n\n" +
		"commit-id:abcd1234\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	assert.Equal(t, "fedcba98", commits[0].sprCommitID,
		"the FIRST commit-id-looking thing in the description wins (current behavior)")
}

func TestParseJjLogOutput_TrailerTooShort(t *testing.T) {
	// 7 hex chars doesn't match (need exactly 8 in the regex).
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\ncommit-id:abcdef1\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.False(t, valid)
	assert.Empty(t, commits[0].sprCommitID)
}

func TestParseJjLogOutput_TrailerTooLong_FirstEightWin(t *testing.T) {
	// 12 hex chars: regex captures the first 8 only.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\ncommit-id:0123456789ab\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	assert.Equal(t, "01234567", commits[0].sprCommitID)
}

func TestParseJjLogOutput_TrailerNonHexIgnored(t *testing.T) {
	// `xyz12345` is not all hex → no match → invalid.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\ncommit-id:xyz12345\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.False(t, valid)
	assert.Empty(t, commits[0].sprCommitID)
}

func TestParseJjLogOutput_DescriptionWithCRLF(t *testing.T) {
	// Some editors / pipes may produce CRLF. TrimSpace and the content-
	// based regex handle this gracefully.
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\r\n\r\nbody\r\n\r\ncommit-id:abcd1234\r\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid, "CRLF in description must not break trailer detection")
	assert.Equal(t, "abcd1234", commits[0].sprCommitID)
	// Subject should not contain the trailing CR.
	assert.Equal(t, "subject", commits[0].subject)
}

// TestParseJjLogOutput_TrailerStrippedFromBody pins that the commit-id
// trailer line is removed from Commit.Body so PR templates that embed
// the body don't surface spr's internal trailer. Matches git-mode
// behavior in git/helpers.go::parseLocalCommitStack.
func TestParseJjLogOutput_TrailerStrippedFromBody(t *testing.T) {
	input := "abcdef0123\x1fchange\x1ffalse\x1fsubject\n\nReal body text.\n\nMore body text.\n\ncommit-id:abcd1234\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "abcd1234", commits[0].sprCommitID)
	assert.Equal(t, "Real body text.\n\nMore body text.", commits[0].body,
		"Body must not include the commit-id trailer line")
}

func TestParseJjLogOutput_MalformedRecord_Skipped(t *testing.T) {
	// First record has only one field (no \x1f separators); parser skips
	// it via the `len(fields) < 4` defensive guard. Second record is
	// well-formed and should be retained.
	input := "garbage-record-with-no-separators\x1e" +
		"abcdef0123\x1fchange\x1ffalse\x1fok\n\ncommit-id:abcd1234\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1, "malformed record must be skipped, well-formed one retained")
	assert.Equal(t, "abcd1234", commits[0].sprCommitID)
}

func TestParseJjLogOutput_MixedValidAndInvalid(t *testing.T) {
	// First commit has trailer, second doesn't
	input := "c100000000000000000000000000000000000000\x1fchange1\x1ffalse\x1fcommit 1\n\ncommit-id:00000001\n\x1e" +
		"c200000000000000000000000000000000000000\x1fchange2\x1ffalse\x1fcommit 2 no trailer\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.False(t, valid) // invalid because second commit lacks trailer
	require.Len(t, commits, 2)
	assert.Equal(t, "00000001", commits[0].sprCommitID)
	assert.Equal(t, "", commits[1].sprCommitID)
}

// TestParseJjLogOutput_NonHexCommitHash_Skipped pins the defensive
// non-hex-SHA guard. If a commit description somehow contains a literal
// \x1f, SplitN shifts fields and field[0] (where we expect the commit
// hash) holds prose. We skip such records rather than letting garbage
// into the parsed stack.
func TestParseJjLogOutput_NonHexCommitHash_Skipped(t *testing.T) {
	input := "not-a-hex-sha-at-all\x1fchange1\x1ffalse\x1fdesc\n\ncommit-id:00000001\n\x1e" +
		"c200000000000000000000000000000000000000\x1fchange2\x1ffalse\x1fok\n\ncommit-id:00000002\n\x1e"
	commits, valid := parseJjLogOutput(input)
	require.True(t, valid)
	require.Len(t, commits, 1, "non-hex commit hash must be skipped, well-formed record retained")
	assert.Equal(t, "00000002", commits[0].sprCommitID)
}

func TestIsHexSHA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"c100000000000000000000000000000000000000", true},
		{"abcdef01", true},  // exactly 8
		{"abcdef0", false},  // too short
		{"abcdef0Z", false}, // contains non-hex
		{"ABCDEF01", false}, // uppercase (jj emits lowercase)
		{"", false},
		{"not a sha", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, isHexSHA(tc.in))
		})
	}
}

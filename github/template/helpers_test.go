package template

import (
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/stretchr/testify/assert"
)

func TestParsePRMetadata_TitleOnly(t *testing.T) {
	body := "some text\nspr-pr-title: My Custom Title\ncommit-id:abc12345"
	meta := ParsePRMetadata(body)
	assert.Equal(t, "My Custom Title", meta.Title)
	assert.Equal(t, "", meta.Description)
}

func TestParsePRMetadata_TitleAndBody(t *testing.T) {
	body := `some text

spr-pr-title: My PR Title
spr-pr-body: This is the PR description.

It has multiple paragraphs.
---end-spr-pr-body

commit-id:abc12345`

	meta := ParsePRMetadata(body)
	assert.Equal(t, "My PR Title", meta.Title)
	assert.Equal(t, "This is the PR description.\n\nIt has multiple paragraphs.", meta.Description)
}

func TestParsePRMetadata_BodyWithoutEndMarker(t *testing.T) {
	body := `spr-pr-body: Description text.

More text.

commit-id:abc12345`

	meta := ParsePRMetadata(body)
	assert.Equal(t, "", meta.Title)
	assert.Equal(t, "Description text.\n\nMore text.", meta.Description)
}

func TestParsePRMetadata_NoMarkers(t *testing.T) {
	body := "just a normal commit body\n\ncommit-id:abc12345"
	meta := ParsePRMetadata(body)
	assert.Equal(t, "", meta.Title)
	assert.Equal(t, "", meta.Description)
}

func TestParsePRMetadata_Empty(t *testing.T) {
	meta := ParsePRMetadata("")
	assert.Equal(t, "", meta.Title)
	assert.Equal(t, "", meta.Description)
}

func TestParsePRMetadata_BodyOnSameLine(t *testing.T) {
	body := "spr-pr-body: Single line description\n---end-spr-pr-body\ncommit-id:abc12345"
	meta := ParsePRMetadata(body)
	assert.Equal(t, "Single line description", meta.Description)
}

func TestFormatGroupCommits_MultipleCommits(t *testing.T) {
	commits := []git.Commit{
		{Subject: "Add feature A", Body: "Details about A\n\ncommit-id:00000001"},
		{Subject: "Add feature B", Body: "commit-id:00000002"},
	}

	result := FormatGroupCommits(commits)
	assert.Contains(t, result, "### Add feature A")
	assert.Contains(t, result, "Details about A")
	assert.Contains(t, result, "### Add feature B")
	// commit-id should be stripped
	assert.NotContains(t, result, "commit-id:")
}

func TestFormatGroupCommits_SingleCommit(t *testing.T) {
	commits := []git.Commit{
		{Subject: "Only commit", Body: ""},
	}

	result := FormatGroupCommits(commits)
	assert.Contains(t, result, "### Only commit")
}

func TestStripSPRMetadata(t *testing.T) {
	body := `Some description

spr-pr-title: Title
spr-pr-body: Body text
---end-spr-pr-body

commit-id:abc12345`

	result := StripSPRMetadata(body)
	assert.Equal(t, "Some description", result)
	assert.NotContains(t, result, "spr-pr-title")
	assert.NotContains(t, result, "spr-pr-body")
	assert.NotContains(t, result, "commit-id")
}

func TestStripSPRMetadata_NoMetadata(t *testing.T) {
	body := "Just a normal body\n\nWith paragraphs"
	result := StripSPRMetadata(body)
	assert.Equal(t, "Just a normal body\n\nWith paragraphs", result)
}

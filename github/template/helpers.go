package template

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
)

/*
func AddManualMergeNotice(body string) string {
	return body + "\n\n" +
		"⚠️ *Part of a stack created by [spr](https://github.com/ejoffe/spr). " +
		"Do not merge manually using the UI - doing so may have unexpected results.*"
}
*/

func ManualMergeNotice() string {
	return "⚠️ *Part of a stack created by [spr](https://github.com/ejoffe/spr). " +
		"Do not merge manually using the UI - doing so may have unexpected results.*"
}

// PRMetadata holds optional PR title/body overrides parsed from commit messages.
type PRMetadata struct {
	Title       string // from spr-pr-title: marker, or ""
	Description string // from spr-pr-body: marker, or ""
}

// ParsePRMetadata extracts optional spr-pr-title: and spr-pr-body: markers
// from a commit message body.
//
// spr-pr-title: is a single-line value.
// spr-pr-body: spans from the marker to ---end-spr-pr-body or end of text
// (whichever comes first), excluding other trailers like commit-id:.
func ParsePRMetadata(body string) PRMetadata {
	var meta PRMetadata
	lines := strings.Split(body, "\n")

	inBody := false
	var bodyLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "spr-pr-title:") {
			meta.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "spr-pr-title:"))
			continue
		}

		if strings.HasPrefix(trimmed, "spr-pr-body:") {
			inBody = true
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "spr-pr-body:"))
			if rest != "" {
				bodyLines = append(bodyLines, rest)
			}
			continue
		}

		if inBody {
			if trimmed == "---end-spr-pr-body" {
				inBody = false
				continue
			}
			// Stop at known trailers
			if strings.HasPrefix(trimmed, "commit-id:") {
				inBody = false
				continue
			}
			bodyLines = append(bodyLines, line)
		}
	}

	if len(bodyLines) > 0 {
		meta.Description = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	}

	return meta
}

// FormatGroupCommits formats a list of commits for inclusion in a multi-commit PR body.
// Each commit is rendered as a bullet with subject, plus body if non-empty.
func FormatGroupCommits(commits []git.Commit) string {
	var buf bytes.Buffer
	for _, c := range commits {
		// Strip spr metadata lines from display
		cleanBody := StripSPRMetadata(c.Body)
		if cleanBody != "" {
			buf.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", c.Subject, cleanBody))
		} else {
			buf.WriteString(fmt.Sprintf("### %s\n\n", c.Subject))
		}
	}
	return buf.String()
}

// stripSPRMetadata removes spr-pr-title:, spr-pr-body:, commit-id:, and
// ---end-spr-pr-body lines from a commit body for cleaner display.
func StripSPRMetadata(body string) string {
	lines := strings.Split(body, "\n")
	var result []string
	inBody := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "spr-pr-title:") ||
			strings.HasPrefix(trimmed, "commit-id:") {
			continue
		}
		if strings.HasPrefix(trimmed, "spr-pr-body:") {
			inBody = true
			continue
		}
		if inBody {
			if trimmed == "---end-spr-pr-body" {
				inBody = false
			}
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

func FormatStackMarkdown(commit git.Commit, stack []*github.PullRequest, showPrTitlesInStack bool) string {
	var buf bytes.Buffer
	for i := len(stack) - 1; i >= 0; i-- {
		isCurrent := stack[i].Commit.CommitID == commit.CommitID
		var suffix string
		if isCurrent {
			suffix = " ⬅"
		} else {
			suffix = ""
		}
		var prTitle string
		if showPrTitlesInStack {
			prTitle = fmt.Sprintf("%s ", stack[i].Title)
		} else {
			prTitle = ""
		}

		buf.WriteString(fmt.Sprintf("- %s#%d%s\n", prTitle, stack[i].Number, suffix))
	}

	return buf.String()
}

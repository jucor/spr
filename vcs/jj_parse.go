package vcs

import (
	"regexp"
	"strings"
)

// parsedJjCommit is the intermediate representation of a commit from jj log output.
type parsedJjCommit struct {
	commitHash  string // git SHA (from jj's commit_id template keyword)
	changeID    string // jj change ID
	empty       bool
	description string
	sprCommitID string // extracted from commit-id: trailer, may be ""
	subject     string
	body        string
	wip         bool
}

// parseJjLogOutput parses output from:
//
//	jj log --no-graph --reversed --color=never -r 'trunk()..(@:: | ::@)'
//	  -T 'commit_id ++ "\x1f" ++ change_id ++ "\x1f" ++ empty ++ "\x1f" ++ description ++ "\x1e"'
//
// The connected-component revset is position-independent: it returns the
// whole linear stack regardless of where @ is within it. Fields are
// separated by \x1f (unit separator), records by \x1e (record separator).
// Returns the parsed commits and true if all non-empty commits have
// commit-id trailers.
func parseJjLogOutput(output string) ([]parsedJjCommit, bool) {
	commitIDRegex := regexp.MustCompile(`commit-id:\s*([a-f0-9]{8})`)
	// trailerLineRegex matches a "commit-id:" trailer on its own line (the
	// canonical form, including any surrounding blank lines) so we can
	// strip it from Body. Matching anchored at line start avoids stripping
	// occurrences embedded inside a prose body.
	trailerLineRegex := regexp.MustCompile(`(?m)^\s*commit-id:\s*[a-f0-9]{8}\s*$`)

	records := strings.Split(output, "\x1e")
	var commits []parsedJjCommit
	valid := true

	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.SplitN(record, "\x1f", 4)
		if len(fields) < 4 {
			continue
		}

		commitHash := strings.TrimSpace(fields[0])
		changeID := strings.TrimSpace(fields[1])
		isEmpty := strings.TrimSpace(fields[2]) == "true"
		description := fields[3]

		// Defensive: if a commit description contains a literal \x1f
		// (extremely rare but possible), our SplitN would shift fields
		// and commitHash would no longer look like a hex SHA. Skip such
		// records rather than corrupting the parsed stack — jj emits SHAs
		// as lowercase hex.
		if !isHexSHA(commitHash) {
			continue
		}

		// Skip empty commits with no description (working copy placeholder)
		if isEmpty && strings.TrimSpace(description) == "" {
			continue
		}

		// Parse subject and body from description.
		lines := strings.SplitN(strings.TrimSpace(description), "\n", 2)
		subject := ""
		body := ""
		if len(lines) > 0 {
			subject = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 {
			// Strip the commit-id trailer line so PR templates that
			// embed Commit.Body don't surface spr's internal trailer.
			// Matches git mode (parseLocalCommitStack also keeps the
			// trailer out of Body).
			body = strings.TrimSpace(trailerLineRegex.ReplaceAllString(lines[1], ""))
		}

		// Extract commit-id trailer
		var sprCommitID string
		matches := commitIDRegex.FindStringSubmatch(description)
		if matches != nil {
			sprCommitID = matches[1]
		} else if !isEmpty {
			valid = false
		}

		commits = append(commits, parsedJjCommit{
			commitHash:  commitHash,
			changeID:    changeID,
			empty:       isEmpty,
			description: description,
			sprCommitID: sprCommitID,
			subject:     subject,
			body:        body,
			wip:         strings.HasPrefix(subject, "WIP"),
		})
	}

	return commits, valid
}

// isHexSHA reports whether s looks like a jj commit hash: non-empty
// lowercase hex of at least 8 chars. Defensive guard against record-
// separator collisions in commit descriptions.
func isHexSHA(s string) bool {
	if len(s) < 8 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

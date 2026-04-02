package vcs

import (
	"regexp"
	"strings"

	"github.com/ejoffe/spr/git"
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
	bookmarks   []string // jj bookmarks pointing at this commit
}

// parseJjLogOutputExtended parses output from the extended template that includes bookmarks:
//
//	jj log ... -T 'commit_id ++ "\x1f" ++ change_id ++ "\x1f" ++ empty ++ "\x1f" ++
//	  bookmarks ++ "\x1f" ++ description ++ "\x1e"'
//
// Fields: commit_id, change_id, empty, bookmarks, description (5 fields).
// Returns the parsed commits and true if all non-empty commits have commit-id trailers.
func parseJjLogOutputExtended(output string) ([]parsedJjCommit, bool) {
	commitIDRegex := regexp.MustCompile(`commit-id:\s*([a-f0-9]{8})`)

	records := strings.Split(output, "\x1e")
	var commits []parsedJjCommit
	valid := true

	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		fields := strings.SplitN(record, "\x1f", 5)
		if len(fields) < 5 {
			continue
		}

		commitHash := strings.TrimSpace(fields[0])
		changeID := strings.TrimSpace(fields[1])
		isEmpty := strings.TrimSpace(fields[2]) == "true"
		bookmarksRaw := strings.TrimSpace(fields[3])
		description := fields[4]

		if isEmpty && strings.TrimSpace(description) == "" {
			continue
		}

		// Parse bookmarks (space-separated in jj output, may have trailing markers like *)
		var bookmarks []string
		if bookmarksRaw != "" {
			for _, bm := range strings.Fields(bookmarksRaw) {
				// Strip trailing markers like * (indicating divergent bookmarks)
				bm = strings.TrimRight(bm, "*")
				if bm != "" && !git.IsSPRBranch(bm) {
					bookmarks = append(bookmarks, bm)
				}
			}
		}

		lines := strings.SplitN(strings.TrimSpace(description), "\n", 2)
		subject := ""
		body := ""
		if len(lines) > 0 {
			subject = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}

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
			bookmarks:   bookmarks,
		})
	}

	return commits, valid
}

// parseJjLogOutput parses output from:
//
//	jj log --no-graph --reversed --color=never -r 'trunk()..@'
//	  -T 'commit_id ++ "\x1f" ++ change_id ++ "\x1f" ++ empty ++ "\x1f" ++ description ++ "\x1e"'
//
// Fields are separated by \x1f (unit separator), records by \x1e (record separator).
// Returns the parsed commits and true if all non-empty commits have commit-id trailers.
func parseJjLogOutput(output string) ([]parsedJjCommit, bool) {
	commitIDRegex := regexp.MustCompile(`commit-id:\s*([a-f0-9]{8})`)

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

		// Skip empty commits with no description (working copy placeholder)
		if isEmpty && strings.TrimSpace(description) == "" {
			continue
		}

		// Parse subject and body from description
		lines := strings.SplitN(strings.TrimSpace(description), "\n", 2)
		subject := ""
		body := ""
		if len(lines) > 0 {
			subject = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
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

//go:build integration

package vcs_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/vcs/jjtest"
)

func writeFile(t *testing.T, dir, name, content string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
}

// TestJjMode_CascadeOrphans observes how jj handles the same cascade-orphan
// scenario the git-mode test exercises in vcs/gittest. Mirror setup:
//
//	origin/master = M0                              (initial)
//	local         = M0 → A → B → C → D              (4 commits stacked)
//
// Simulated GitHub mid-stack squash of PR-C: origin advances to a single
// commit S whose tree contains A+B+C's cumulative content. PR-D is still
// open and has NOT been merged.
//
//	origin/master = M0 → S                          (S has file_a/b/c)
//	local         = M0 → A → B → C → D              (unchanged)
//
// The polis bug surfaces when `jj git fetch + jj rebase -b @ -d
// master@origin` (= JjOps.FetchAndRebase) tries to reapply A/B/C onto S.
// jj preserves change IDs and runs a 3-way merge against each commit's
// parent — when the same content appears on both sides of the merge,
// jj currently reports "duplicate change" conflicts rather than dropping
// the commits the way git's patch-id detection does.
//
// This test has two sub-cases:
//
//  1. vanilla: runs JjOps.FetchAndRebase exactly as spr does it today.
//     Observes whether jj produces conflicts, empty commits, or self-heals.
//
//  2. skip_emptied: same setup, but rebases with `--skip-emptied`. Observes
//     whether this flag would make jj's behavior match git's.
//
// Both sub-cases are observational with diagnostic logging plus targeted
// asserts on what we expect (one-of: success with D only, conflict, or
// emptied-commits-left-behind). The point is to learn jj's real behavior
// here before deciding what spr should do.
func TestJjMode_CascadeOrphans(t *testing.T) {
	t.Run("vanilla_rebase", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: false, drift: false})
	})
	t.Run("rebase_skip_emptied", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: true, drift: false})
	})
	// Drift variant: closer to the polis production bug. After the squash
	// has landed on origin, the user (or Claude) edits A's content locally
	// (e.g. tweaking a comment block on a commit they don't yet realize is
	// an orphan). A's content has now drifted from what's inside S. The
	// next fetch+rebase tries to reapply the *drifted* A onto S, producing
	// a real 3-way merge conflict — this is the form the polis stack hit.
	t.Run("vanilla_rebase_with_drift", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: false, drift: true})
	})
	t.Run("rebase_skip_emptied_with_drift", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: true, drift: true})
	})
	// Structural-overlap variant: the form the polis stack actually hit.
	// A/B/C/D each modify the SAME file (cumulative additions). S on
	// origin contains the cumulative diff of A+B+C. No user-edit drift
	// involved — the drift is structural: each local commit's slice of
	// the file's evolution doesn't bit-match any sub-section of S's
	// cumulative diff, so jj's 3-way rebase reports conflicts even though
	// nothing was edited post-merge.
	t.Run("vanilla_rebase_with_structural_overlap", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: false, structural: true})
	})
	t.Run("rebase_skip_emptied_with_structural_overlap", func(t *testing.T) {
		runCascadeScenario(t, cascadeOpts{skipEmptied: true, structural: true})
	})
}

type cascadeOpts struct {
	skipEmptied bool
	drift       bool
	structural  bool
}

// TestJjOps_FetchAndRebase_SelfHealsCleanCascade is the spr-API regression
// guard for the --skip-emptied fix. It runs the clean cascade scenario
// (A/B/C/D with distinct files; S on origin = A+B+C) through the actual
// JjOps.FetchAndRebase entry point and asserts the post-rebase stack has
// only D — matching git mode's self-healing behavior.
//
// If --skip-emptied is ever removed from FetchAndRebase, this test fails
// loudly with A/B/C left as [EMPTY] stubs alongside D. Pairs with the
// characterization sub-tests in TestJjMode_CascadeOrphans which document
// jj's raw behavior independent of the spr API.
func TestJjOps_FetchAndRebase_SelfHealsCleanCascade(t *testing.T) {
	repo := jjtest.NewRepo(t)

	repo.AddCommitFile(t, "A: add file_a", "file_a.txt", "a-content\n", true)
	repo.AddCommitFile(t, "B: add file_b", "file_b.txt", "b-content\n", true)
	repo.AddCommitFile(t, "C: add file_c", "file_c.txt", "c-content\n", true)
	repo.AddCommitFile(t, "D: add file_d", "file_d.txt", "d-content\n", true)

	repo.PushSquashToOrigin(t, "S: squash of A+B+C", map[string]string{
		"file_a.txt": "a-content\n",
		"file_b.txt": "b-content\n",
		"file_c.txt": "c-content\n",
	})

	if err := repo.JjOps.FetchAndRebase(repo.Cfg, nil); err != nil {
		t.Fatalf("FetchAndRebase: %v", err)
	}

	rows := parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")
	var stack []jjRow
	for _, r := range rows {
		if r.desc != "" {
			stack = append(stack, r)
		}
	}
	if len(stack) != 1 {
		t.Fatalf("expected exactly 1 user-stack commit after self-heal (D only), got %d:\n%s",
			len(stack), formatJjRows(rows))
	}
	if !strings.HasPrefix(stack[0].desc, "D:") {
		t.Errorf("expected D to be the surviving commit, got %q", stack[0].desc)
	}
	if stack[0].empty {
		t.Errorf("D should not be empty after rebase; row: %+v", stack[0])
	}
	if stack[0].conflict {
		t.Errorf("D should not be in conflict after rebase; row: %+v", stack[0])
	}
}

// TestJjOps_FetchAndRebase_OrphanPrune_ResolvesDrift exercises the
// orphan-abandon path on the drift scenario (orphan commit amended
// locally after S landed). Vanilla rebase + --skip-emptied alone leaves
// A conflicted and D inheriting; passing A as an orphan into
// FetchAndRebase abandons it before rebase, so D rebases clean onto S.
func TestJjOps_FetchAndRebase_OrphanPrune_ResolvesDrift(t *testing.T) {
	repo := jjtest.NewRepo(t)

	a := repo.AddCommitFile(t, "A: add file_a", "file_a.txt", "a-content\n", true)
	repo.AddCommitFile(t, "B: add file_b", "file_b.txt", "b-content\n", true)
	repo.AddCommitFile(t, "C: add file_c", "file_c.txt", "c-content\n", true)
	d := repo.AddCommitFile(t, "D: add file_d", "file_d.txt", "d-content\n", true)

	repo.PushSquashToOrigin(t, "S: squash of A+B+C", map[string]string{
		"file_a.txt": "a-content\n",
		"file_b.txt": "b-content\n",
		"file_c.txt": "c-content\n",
	})

	// Drift injection — same shape as TestJjMode_CascadeOrphans drift variant.
	repo.Edit(t, a)
	if err := writeFile(t, repo.Path, "file_a.txt", "a-content-AMENDED\n"); err != nil {
		t.Fatalf("amend file_a.txt: %v", err)
	}
	repo.Edit(t, d)

	// Pass A as an orphan; B and C don't need to be passed because
	// --skip-emptied will drop them (their content is fully in S).
	if err := repo.JjOps.FetchAndRebase(repo.Cfg, []string{a}); err != nil {
		t.Fatalf("FetchAndRebase with orphan A: %v", err)
	}

	rows := parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")
	t.Logf("post-rebase stack:\n%s", formatJjRows(rows))

	var stack []jjRow
	for _, r := range rows {
		if r.desc != "" {
			stack = append(stack, r)
		}
	}
	if len(stack) != 1 {
		t.Fatalf("expected exactly 1 user-stack commit (D only), got %d:\n%s",
			len(stack), formatJjRows(rows))
	}
	if !strings.HasPrefix(stack[0].desc, "D:") {
		t.Errorf("expected D to survive, got %q", stack[0].desc)
	}
	if stack[0].conflict {
		t.Errorf("D should not be in conflict after orphan-abandon resolves drift; row: %+v", stack[0])
	}
}

// TestJjOps_FetchAndRebase_OrphanPrune_ResolvesStructuralOverlap is the
// closest match to the polis production form. All four PRs modify the
// same shared file with cumulative additions; S contains the A+B+C
// cumulative diff. Without orphan-abandon, A and B conflict on rebase
// even with --skip-emptied. With A/B/C passed as orphans, the rebase
// only places D on top of S — clean.
func TestJjOps_FetchAndRebase_OrphanPrune_ResolvesStructuralOverlap(t *testing.T) {
	repo := jjtest.NewRepo(t)

	const lineA = "from-A: contribution\n"
	const lineB = "from-B: contribution\n"
	const lineC = "from-C: contribution\n"
	const lineD = "from-D: contribution\n"

	a := repo.AddCommitFile(t, "A: init journal.md", "journal.md", lineA, true)
	b := repo.AddCommitFile(t, "B: extend journal.md", "journal.md", lineA+lineB, true)
	c := repo.AddCommitFile(t, "C: extend journal.md", "journal.md", lineA+lineB+lineC, true)
	_ = repo.AddCommitFile(t, "D: extend journal.md", "journal.md", lineA+lineB+lineC+lineD, true)

	repo.PushSquashToOrigin(t, "S: squash of A+B+C (PR-C merged)", map[string]string{
		"journal.md": lineA + lineB + lineC,
	})

	if err := repo.JjOps.FetchAndRebase(repo.Cfg, []string{a, b, c}); err != nil {
		t.Fatalf("FetchAndRebase with orphan A/B/C: %v", err)
	}

	rows := parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")
	t.Logf("post-rebase stack:\n%s", formatJjRows(rows))

	var stack []jjRow
	for _, r := range rows {
		if r.desc != "" {
			stack = append(stack, r)
		}
	}
	if len(stack) != 1 {
		t.Fatalf("expected exactly 1 user-stack commit (D only), got %d:\n%s",
			len(stack), formatJjRows(rows))
	}
	if !strings.HasPrefix(stack[0].desc, "D:") {
		t.Errorf("expected D to survive, got %q", stack[0].desc)
	}
	if stack[0].conflict {
		t.Errorf("D should not be in conflict after orphan-abandon resolves structural overlap; row: %+v", stack[0])
	}
}

func runCascadeScenario(t *testing.T, opts cascadeOpts) {
	skipEmptied := opts.skipEmptied
	repo := jjtest.NewRepo(t)

	// Build the local stack and compute the squash content for S, depending
	// on the scenario shape.
	//
	//   - Default (distinct files): each PR adds its own file. The squash on
	//     origin is bit-identical to each local commit's diff. This is the
	//     "clean" form — exposes the orphan-empties variant of the bug.
	//
	//   - Structural overlap (the polis form): all four PRs modify the SAME
	//     file with cumulative additions. The squash on origin contains the
	//     cumulative diff of A+B+C. Each local commit's slice is a subset of
	//     that cumulative diff, but not bit-identical to any single section.
	//     jj's 3-way rebase reports conflicts because both source (local
	//     commit) and target (S) modify the file from the same base, with
	//     overlapping but-different contents.
	var a, b, c, d string
	var squashFiles map[string]string
	if opts.structural {
		// Shared file with cumulative additions. A creates the file with
		// just A's section; B appends B's section; C appends C's; D appends
		// D's. S contains journal.md with A+B+C's sections (no D yet).
		const lineA = "from-A: contribution\n"
		const lineB = "from-B: contribution\n"
		const lineC = "from-C: contribution\n"
		const lineD = "from-D: contribution\n"
		a = repo.AddCommitFile(t, "A: init journal.md", "journal.md", lineA, true)
		b = repo.AddCommitFile(t, "B: extend journal.md", "journal.md", lineA+lineB, true)
		c = repo.AddCommitFile(t, "C: extend journal.md", "journal.md", lineA+lineB+lineC, true)
		d = repo.AddCommitFile(t, "D: extend journal.md", "journal.md", lineA+lineB+lineC+lineD, true)
		squashFiles = map[string]string{
			"journal.md": lineA + lineB + lineC,
		}
	} else {
		// Distinct files per commit — the "clean" cascade form.
		a = repo.AddCommitFile(t, "A: add file_a", "file_a.txt", "a-content\n", true)
		b = repo.AddCommitFile(t, "B: add file_b", "file_b.txt", "b-content\n", true)
		c = repo.AddCommitFile(t, "C: add file_c", "file_c.txt", "c-content\n", true)
		d = repo.AddCommitFile(t, "D: add file_d", "file_d.txt", "d-content\n", true)
		squashFiles = map[string]string{
			"file_a.txt": "a-content\n",
			"file_b.txt": "b-content\n",
			"file_c.txt": "c-content\n",
		}
	}
	t.Logf("local stack built (structural=%v): A=%s B=%s C=%s D=%s",
		opts.structural, a, b, c, d)
	t.Logf("local jj log before fetch:\n%s",
		formatJjRows(parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")))

	// Simulate GitHub squash-merge of PR-C: push a single commit S to origin
	// whose tree contains the squash content computed above.
	s := repo.PushSquashToOrigin(t, "S: squash of A+B+C (PR-C merged on GitHub)", squashFiles)
	t.Logf("origin/master advanced to S=%s (contains A+B+C content)", s)

	// Drift injection (only in the distinct-files scenario where it's
	// meaningful): move @ onto A, rewrite file_a.txt to different bytes
	// (jj auto-snapshots into A), then move @ back to D. Simulates a user
	// editing an orphan commit they don't realize is already orphaned.
	if opts.drift && !opts.structural {
		repo.Edit(t, a)
		if err := writeFile(t, repo.Path, "file_a.txt", "a-content-AMENDED\n"); err != nil {
			t.Fatalf("amend file_a.txt: %v", err)
		}
		repo.Edit(t, d)
		t.Logf("drift injected: A now has file_a.txt = \"a-content-AMENDED\\n\" (S has the original)")
		t.Logf("local jj log after drift, before fetch:\n%s",
			formatJjRows(parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")))
	}

	// Both paths now invoke raw `jj git fetch + jj rebase` directly so the
	// test characterizes jj's behavior independently of JjOps.FetchAndRebase
	// (which we are about to modify in production). The only difference is
	// whether we pass --skip-emptied.
	rebaseArgs := []string{"rebase", "-b", "@", "-d",
		fmt.Sprintf("%s@%s", repo.Cfg.Repo.GitHubBranch, repo.Cfg.Repo.GitHubRemote)}
	if skipEmptied {
		rebaseArgs = append(rebaseArgs, "--skip-emptied")
	}
	fetchRebaseErr := runJj(t, repo.Path, "git", "fetch")
	if fetchRebaseErr == nil {
		fetchRebaseErr = runJj(t, repo.Path, rebaseArgs...)
	}
	t.Logf("fetch+rebase (skip_emptied=%v) returned err=%v", skipEmptied, fetchRebaseErr)

	// Observe the post-rebase stack. The structured parse lets us cleanly
	// distinguish "real" commits (with descriptions) from the trailing
	// empty working-copy commit that jjtest leaves on top via `jj new`.
	rows := parseJjStack(t, repo.Path, "trunk()..(@:: | ::@)")
	t.Logf("jj log -r 'trunk()..(@:: | ::@)' after rebase:\n%s", formatJjRows(rows))

	// Filter out the trailing empty working copy (description empty). What
	// remains is the user-meaningful stack content.
	var stack []jjRow
	for _, r := range rows {
		if r.desc != "" {
			stack = append(stack, r)
		}
	}

	orphanEmpties := 0
	conflicted := 0
	hasD := false
	for _, r := range stack {
		if r.empty {
			orphanEmpties++
		}
		if r.conflict {
			conflicted++
		}
		if strings.HasPrefix(r.desc, "D:") {
			hasD = true
		}
	}
	t.Logf("post-rebase user-stack: %d commits, %d conflicted, %d orphan-emptied",
		len(stack), conflicted, orphanEmpties)

	switch {
	case opts.structural && !skipEmptied:
		// STRUCTURAL OVERLAP + vanilla: closest match to the actual polis
		// production bug (the form that conflicted PRs #2512 and #2513).
		// Expect at least one conflicted commit because the cumulative
		// squash diff in S overlaps with each local commit's slice.
		if conflicted == 0 {
			t.Errorf("structural+vanilla: expected at least 1 conflicted commit, got 0; user-stack:\n%s\nrebase err=%v",
				formatJjRows(stack), fetchRebaseErr)
		}
		t.Logf("STRUCTURAL+VANILLA OUTCOME: %d conflicted, %d orphan-emptied, rebase err=%v — this is the polis production form",
			conflicted, orphanEmpties, fetchRebaseErr)

	case opts.structural && skipEmptied:
		// STRUCTURAL OVERLAP + --skip-emptied: confirms --skip-emptied is
		// insufficient for this form of the bug too. We expect conflicts
		// to remain (since A is not empty — its slice still differs from S).
		t.Logf("STRUCTURAL+SKIP_EMPTIED OUTCOME: %d conflicted, %d orphan-emptied, rebase err=%v",
			conflicted, orphanEmpties, fetchRebaseErr)
		if conflicted == 0 && fetchRebaseErr == nil {
			t.Logf("note: --skip-emptied unexpectedly resolved structural overlap — investigate; user-stack:\n%s",
				formatJjRows(stack))
		}

	case !opts.drift && skipEmptied:
		// CLEAN + --skip-emptied: should match git mode's outcome —
		// exactly D survives.
		if fetchRebaseErr != nil {
			t.Errorf("rebase --skip-emptied should succeed in clean cascade-orphan case, got err=%v", fetchRebaseErr)
		}
		if conflicted != 0 {
			t.Errorf("rebase --skip-emptied should produce no conflicts, got %d", conflicted)
		}
		if orphanEmpties != 0 {
			t.Errorf("rebase --skip-emptied should drop A/B/C, got %d orphan-emptied commits:\n%s",
				orphanEmpties, formatJjRows(stack))
		}
		if len(stack) != 1 {
			t.Errorf("expected exactly 1 user-stack commit (D), got %d:\n%s",
				len(stack), formatJjRows(stack))
		}
		if !hasD {
			t.Errorf("expected D to survive the rebase; user-stack:\n%s", formatJjRows(stack))
		}

	case !opts.drift && !skipEmptied:
		// CLEAN + vanilla: characterizes today's broken behavior — rebase
		// succeeds, A/B/C are left as orphan empties alongside D. If jj's
		// defaults ever change to drop these automatically, this assertion
		// catches it (and we can simplify spr's design).
		if fetchRebaseErr != nil {
			t.Fatalf("clean vanilla rebase unexpectedly errored: %v\n%s",
				fetchRebaseErr, formatJjRows(rows))
		}
		if orphanEmpties == 0 {
			t.Errorf("clean vanilla rebase unexpectedly self-healed; user-stack:\n%s",
				formatJjRows(stack))
		}
		if !hasD {
			t.Errorf("D should still be in the user-stack after clean vanilla rebase; got:\n%s",
				formatJjRows(stack))
		}
		t.Logf("VANILLA(clean) OUTCOME: %d orphan-emptied commits (A/B/C) left alongside D — this is the cascade-orphan empty-commits bug",
			orphanEmpties)

	case opts.drift && !skipEmptied:
		// DRIFT + vanilla: closest match to the polis production bug.
		// Expectation: the amended A's add-file-a patch differs from S's
		// add-file-a content; the 3-way rebase produces a CONFLICT on A.
		// B/C may or may not conflict downstream. The point is to confirm
		// vanilla rebase reaches a stuck state, so we know spr can't ship
		// fetch+rebase without help in this case.
		if conflicted == 0 {
			t.Errorf("drift+vanilla rebase: expected at least 1 conflicted commit, got 0; user-stack:\n%s\nrebase err=%v",
				formatJjRows(stack), fetchRebaseErr)
		}
		t.Logf("DRIFT+VANILLA OUTCOME: %d conflicted, %d orphan-emptied, rebase err=%v — matches the polis stuck-state",
			conflicted, orphanEmpties, fetchRebaseErr)

	case opts.drift && skipEmptied:
		// DRIFT + --skip-emptied: the open question. --skip-emptied only
		// drops commits that ended up empty after rebase. A is NOT empty
		// (its drifted content still differs from S), so it should still
		// conflict. Confirming this here tells us: --skip-emptied alone
		// is insufficient for the drift case; spr will need a complementary
		// path (e.g. detect orphan PRs and `jj abandon` them before fetch,
		// or surface the drift to the user before merging mid-stack).
		t.Logf("DRIFT+SKIP_EMPTIED OUTCOME: %d conflicted, %d orphan-emptied, rebase err=%v",
			conflicted, orphanEmpties, fetchRebaseErr)
		if conflicted == 0 && fetchRebaseErr == nil {
			t.Logf("note: --skip-emptied unexpectedly resolved drift — investigate; user-stack:\n%s",
				formatJjRows(stack))
		}
	}
}

// jjRow is a parsed line from `jj log` with structured fields. Used so
// asserts don't have to do regex sniffing over rendered output (which
// would conflate the trailing empty WC with orphan-emptied commits).
type jjRow struct {
	changeID string
	commitID string
	conflict bool
	empty    bool
	desc     string // first line of description; empty for the WC
}

func parseJjStack(t *testing.T, dir, revset string) []jjRow {
	t.Helper()
	tmpl := `change_id.short() ++ "\x1f" ++ commit_id.short() ++ "\x1f" ++ ` +
		`if(conflict, "Y", "N") ++ "\x1f" ++ if(empty, "Y", "N") ++ "\x1f" ++ ` +
		`description.first_line() ++ "\n"`
	cmd := exec.Command("jj", "log",
		"--no-graph", "--color=never",
		"-r", revset,
		"-T", tmpl,
	)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("jj log parse failed: %v\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}
	var rows []jjRow
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if len(parts) != 5 {
			t.Fatalf("malformed jj log row (want 5 fields, got %d): %q", len(parts), line)
		}
		rows = append(rows, jjRow{
			changeID: parts[0],
			commitID: parts[1],
			conflict: parts[2] == "Y",
			empty:    parts[3] == "Y",
			desc:     strings.TrimSpace(parts[4]),
		})
	}
	return rows
}

func formatJjRows(rows []jjRow) string {
	var b strings.Builder
	for _, r := range rows {
		flags := ""
		if r.conflict {
			flags += "[CONFLICT] "
		}
		if r.empty {
			flags += "[EMPTY] "
		}
		desc := r.desc
		if desc == "" {
			desc = "(no description — working copy)"
		}
		fmt.Fprintf(&b, "  %s %s %s%s\n", r.changeID, r.commitID, flags, desc)
	}
	return b.String()
}

// runJj runs `jj <args...>` in dir and returns the wrapped error (or nil).
func runJj(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("jj", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("jj %s: %w\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return nil
}

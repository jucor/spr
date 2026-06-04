// coverage-merge merges two Go coverprofile files and prints per-file
// attribution showing which lines are covered by each input suite.
//
// Usage:
//
//	go run ./scripts/coverage-merge cov-unit.out cov-integration.out cov-merged.out
//
// The merged profile takes the max hit count per (file, block) so it can be
// fed into `go tool cover -func` / `-html`. The stdout report lists each
// file with three coverage percentages: unit, integration, and merged.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// block represents one coverage block in a coverprofile file.
//
//	file.go:startLine.startCol,endLine.endCol  numStatements  hitCount
type block struct {
	file      string
	rangePart string // "startLine.startCol,endLine.endCol"
	numStmts  int
	hits      int
}

func (b block) key() string { return b.file + ":" + b.rangePart }

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: coverage-merge <unit.out> <integration.out> <merged.out>")
		os.Exit(2)
	}
	unitPath, integPath, mergedPath := os.Args[1], os.Args[2], os.Args[3]

	unit, mode1, err := readProfile(unitPath)
	check(err)
	integ, mode2, err := readProfile(integPath)
	check(err)
	if mode1 != mode2 {
		fmt.Fprintf(os.Stderr, "warning: mode mismatch (%q vs %q); using %q\n", mode1, mode2, mode1)
	}
	mode := mode1
	if mode == "" {
		mode = mode2
	}
	if mode == "" {
		mode = "set"
	}

	// Merge: for each block key, take max(hits) across both inputs.
	merged := map[string]block{}
	for k, b := range unit {
		merged[k] = b
	}
	for k, b := range integ {
		if existing, ok := merged[k]; ok {
			if b.hits > existing.hits {
				existing.hits = b.hits
				merged[k] = existing
			}
		} else {
			merged[k] = b
		}
	}

	if err := writeProfile(mergedPath, mode, merged); err != nil {
		check(err)
	}

	// Build the per-file report.
	files := map[string]bool{}
	for k := range merged {
		files[blockFile(k)] = true
	}
	sorted := make([]string, 0, len(files))
	for f := range files {
		sorted = append(sorted, f)
	}
	sort.Strings(sorted)

	fmt.Println()
	fmt.Println("=== Coverage by file (statement-weighted %) ===")
	fmt.Printf("%-60s  %7s  %7s  %7s\n", "FILE", "UNIT%", "INTEG%", "MERGED%")
	fmt.Println(strings.Repeat("-", 60+3+7+2+7+2+7))
	for _, f := range sorted {
		u := pct(unit, f)
		i := pct(integ, f)
		m := pct(merged, f)
		fmt.Printf("%-60s  %6.1f%%  %6.1f%%  %6.1f%%\n", f, u, i, m)
	}

	// Totals.
	fmt.Println(strings.Repeat("-", 60+3+7+2+7+2+7))
	fmt.Printf("%-60s  %6.1f%%  %6.1f%%  %6.1f%%\n",
		"TOTAL",
		pctAll(unit), pctAll(integ), pctAll(merged))
	fmt.Println()

	// "Only in" sections: files exclusively covered by one suite.
	onlyUnit, onlyInteg := exclusivelyCovered(unit, integ)
	if len(onlyUnit) > 0 {
		fmt.Println("Files covered ONLY by unit tests:")
		for _, f := range onlyUnit {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println()
	}
	if len(onlyInteg) > 0 {
		fmt.Println("Files covered ONLY by integration tests:")
		for _, f := range onlyInteg {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println()
	}
}

func readProfile(path string) (map[string]block, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	blocks := map[string]block{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var mode string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
			continue
		}
		if line == "" {
			continue
		}
		// Format: file.go:1.2,3.4 2 1
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, "", fmt.Errorf("bad coverage line in %s: %q", path, line)
		}
		// fields[0] = "file.go:startLine.startCol,endLine.endCol"
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			return nil, "", fmt.Errorf("bad coverage line in %s: %q", path, line)
		}
		file := fields[0][:colon]
		rangePart := fields[0][colon+1:]
		nstmt, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, "", err
		}
		hits, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, "", err
		}
		b := block{file: file, rangePart: rangePart, numStmts: nstmt, hits: hits}
		// With -coverpkg=./... a single profile may contain multiple
		// entries for the same block (one per test package run). Take
		// the max hit count so we don't lose coverage recorded by an
		// earlier package.
		if existing, ok := blocks[b.key()]; ok && existing.hits > b.hits {
			continue
		}
		blocks[b.key()] = b
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return blocks, mode, nil
}

func writeProfile(path, mode string, blocks map[string]block) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	fmt.Fprintf(w, "mode: %s\n", mode)
	keys := make([]string, 0, len(blocks))
	for k := range blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := blocks[k]
		fmt.Fprintf(w, "%s:%s %d %d\n", b.file, b.rangePart, b.numStmts, b.hits)
	}
	return nil
}

func blockFile(key string) string {
	colon := strings.LastIndex(key, ":")
	if colon < 0 {
		return key
	}
	return key[:colon]
}

// pct returns the statement-weighted percentage of covered blocks in `file`.
func pct(blocks map[string]block, file string) float64 {
	var total, covered int
	for _, b := range blocks {
		if b.file != file {
			continue
		}
		total += b.numStmts
		if b.hits > 0 {
			covered += b.numStmts
		}
	}
	if total == 0 {
		return 0
	}
	return 100 * float64(covered) / float64(total)
}

func pctAll(blocks map[string]block) float64 {
	var total, covered int
	for _, b := range blocks {
		total += b.numStmts
		if b.hits > 0 {
			covered += b.numStmts
		}
	}
	if total == 0 {
		return 0
	}
	return 100 * float64(covered) / float64(total)
}

// exclusivelyCovered returns files where ONE suite has any covered statement
// and the OTHER suite has zero covered statements.
func exclusivelyCovered(a, b map[string]block) (onlyA, onlyB []string) {
	files := map[string]bool{}
	for _, blk := range a {
		files[blk.file] = true
	}
	for _, blk := range b {
		files[blk.file] = true
	}
	for f := range files {
		aCov := pct(a, f) > 0
		bCov := pct(b, f) > 0
		if aCov && !bCov {
			onlyA = append(onlyA, f)
		} else if bCov && !aCov {
			onlyB = append(onlyB, f)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

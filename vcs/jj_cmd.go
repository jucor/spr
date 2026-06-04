package vcs

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"
)

// JjCmd executes jj commands, mirroring the git/realgit/realcmd.go pattern.
type JjCmd struct {
	rootdir string
}

// NewJjCmd creates a new jj command executor.
func NewJjCmd(rootDir string) *JjCmd {
	return &JjCmd{rootdir: rootDir}
}

// JjInterface abstracts jj command execution for testing.
type JjInterface interface {
	Jj(args string, output *string) error
	MustJj(args string, output *string)
	JjArgs(args []string, output *string) error
}

// Jj executes a jj command with the given arguments.
//
// stdout and stderr are captured separately so jj's informational messages
// (e.g. "Rebased N descendant commits onto updated working copy", which jj
// emits on stderr after operations that touched the graph) don't corrupt
// the parsed stdout — without this separation, the next command's output
// would have those messages prepended and parsers like parseJjLogOutput
// would silently produce garbage.
func (c *JjCmd) Jj(args string, output *string) error {
	log.Debug().Msgf("jj %s", args)
	cmdArgs := strings.Fields(args)
	cmd := exec.Command("jj", cmdArgs...)
	cmd.Dir = c.rootdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if output != nil {
		*output = strings.TrimRight(stdout.String(), "\n")
	}
	if err != nil {
		return fmt.Errorf("jj %s: %w\nstdout: %s\nstderr: %s",
			args, err, stdout.String(), stderr.String())
	}
	return nil
}

// MustJj executes a jj command and panics on error.
func (c *JjCmd) MustJj(args string, output *string) {
	err := c.Jj(args, output)
	if err != nil {
		panic(err)
	}
}

// JjArgs executes a jj command with pre-split arguments (for messages with spaces).
// Same stdout/stderr separation as [JjCmd.Jj] — see its docstring.
func (c *JjCmd) JjArgs(args []string, output *string) error {
	log.Debug().Msgf("jj %v", args)
	cmd := exec.Command("jj", args...)
	cmd.Dir = c.rootdir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if output != nil {
		*output = strings.TrimRight(stdout.String(), "\n")
	}
	if err != nil {
		return fmt.Errorf("jj %v: %w\nstdout: %s\nstderr: %s",
			args, err, stdout.String(), stderr.String())
	}
	return nil
}

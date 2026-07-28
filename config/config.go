package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ejoffe/rake"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
)

type Config struct {
	Repo  *RepoConfig
	User  *UserConfig
	State *InternalState
}

// Config object to hold spr configuration
type RepoConfig struct {
	GitHubRepoOwner string `yaml:"githubRepoOwner"`
	GitHubRepoName  string `yaml:"githubRepoName"`
	GitHubHost      string `default:"github.com" yaml:"githubHost"`

	GitHubRemote string `default:"origin" yaml:"githubRemote"`
	GitHubBranch string `default:"main" yaml:"githubBranch"`

	RequireChecks    bool     `default:"true" yaml:"requireChecks"`
	RequiredChecks   []string `yaml:"requiredChecks"`
	RequireApproval  bool     `default:"true" yaml:"requireApproval"`
	DefaultReviewers []string `yaml:"defaultReviewers"`

	MergeMethod string `default:"rebase" yaml:"mergeMethod"`
	MergeQueue  bool   `default:"false" yaml:"mergeQueue"`

	StackMergeMode    string `default:"tip" yaml:"stackMergeMode"`
	SquashGranularity string `default:"perStack" yaml:"squashGranularity"`
	MergeWaitTimeout  string `default:"10m" yaml:"mergeWaitTimeout"`

	PRTemplateType        string `default:"stack" yaml:"prTemplateType"`
	PRTemplatePath        string `yaml:"prTemplatePath,omitempty"`
	PRTemplateInsertStart string `yaml:"prTemplateInsertStart,omitempty"`
	PRTemplateInsertEnd   string `yaml:"prTemplateInsertEnd,omitempty"`

	MergeCheck string `yaml:"mergeCheck,omitempty"`

	ForceFetchTags bool `default:"false" yaml:"forceFetchTags"`

	ShowPrTitlesInStack    bool `default:"false" yaml:"showPrTitlesInStack"`
	BranchPushIndividually bool `default:"false" yaml:"branchPushIndividually"`
}

type UserConfig struct {
	ShowPRLink       bool `default:"true" yaml:"showPRLink"`
	LogGitCommands   bool `default:"true" yaml:"logGitCommands"`
	LogGitHubCalls   bool `default:"true" yaml:"logGitHubCalls"`
	StatusBitsHeader bool `default:"true" yaml:"statusBitsHeader"`
	StatusBitsEmojis bool `default:"true" yaml:"statusBitsEmojis"`

	CreateDraftPRs       bool `default:"false" yaml:"createDraftPRs"`
	PreserveTitleAndBody bool `default:"false" yaml:"preserveTitleAndBody"`
	NoRebase             bool `default:"false" yaml:"noRebase"`
	NoFetch              bool `default:"false" yaml:"noFetch"`
	DeleteMergedBranches bool `default:"false" yaml:"deleteMergedBranches"`
	ShortPRLink          bool `default:"false" yaml:"shortPRLink"`
	ShowCommitID         bool `default:"false" yaml:"showCommitID"`
	BranchPrefix         string `default:"spr" yaml:"branchPrefix"`
}

type InternalState struct {
	MergeCheckCommit   map[string]string `yaml:"mergeCheckCommit"`
	MergeAllInProgress map[string]bool   `yaml:"mergeAllInProgress"`

	Stargazer bool `default:"false" yaml:"stargazer"`
	RunCount  int  `default:"0" yaml:"runcount"`
}

func EmptyConfig() *Config {
	return &Config{
		Repo: &RepoConfig{},
		User: &UserConfig{},
		State: &InternalState{
			MergeCheckCommit:   map[string]string{},
			MergeAllInProgress: map[string]bool{},
		},
	}
}

func DefaultConfig() *Config {
	cfg := EmptyConfig()
	rake.LoadSources(cfg.Repo,
		rake.DefaultSource(),
	)
	rake.LoadSources(cfg.User,
		rake.DefaultSource(),
	)

	cfg.User.LogGitCommands = false
	cfg.User.LogGitHubCalls = false

	// Normalize config (e.g., set PRTemplateType to "custom" if PRTemplatePath is provided)
	cfg.Normalize()

	return cfg
}

// Normalize applies normalization rules to the config
// For example, if PRTemplatePath is provided, PRTemplateType should be set to "custom"
func (c *Config) Normalize() {
	if c.Repo != nil && c.Repo.PRTemplatePath != "" {
		c.Repo.PRTemplateType = "custom"
	}
}

func (c Config) MergeMethod() (genclient.PullRequestMergeMethod, error) {
	var mergeMethod genclient.PullRequestMergeMethod
	var err error
	switch strings.ToLower(c.Repo.MergeMethod) {
	case "merge":
		mergeMethod = genclient.PullRequestMergeMethod_MERGE
	case "squash":
		mergeMethod = genclient.PullRequestMergeMethod_SQUASH
	case "rebase", "":
		mergeMethod = genclient.PullRequestMergeMethod_REBASE
	default:
		err = fmt.Errorf(
			`unknown merge method %q, choose from "merge", "squash", or "rebase"`,
			c.Repo.MergeMethod,
		)
	}
	return mergeMethod, err
}

// MergeAllEnabled reports whether stackMergeMode selects the mergeAll
// behavior (every PR in the stack ends MERGED) instead of the default
// tip behavior (merge top PR, close the rest).
func (c Config) MergeAllEnabled() bool {
	return strings.EqualFold(c.Repo.StackMergeMode, "mergeAll")
}

// SquashPerPR reports whether mergeAll+squash should land one squash
// commit per PR (bottom-up with restacks) instead of one per stack
// (tip-down cascade).
func (c Config) SquashPerPR() bool {
	return strings.EqualFold(c.Repo.SquashGranularity, "perPR")
}

// MergeWaitTimeoutDuration parses mergeWaitTimeout, falling back to 10m
// on empty, invalid, or non-positive values.
func (c Config) MergeWaitTimeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Repo.MergeWaitTimeout)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

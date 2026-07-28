package config

import (
	"testing"
	"time"

	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/stretchr/testify/assert"
)

func TestEmptyConfig(t *testing.T) {
	expect := &Config{
		Repo: &RepoConfig{},
		User: &UserConfig{},
		State: &InternalState{
			MergeCheckCommit: map[string]string{},
		},
	}
	actual := EmptyConfig()
	assert.Equal(t, expect, actual)
}

func TestDefaultConfig(t *testing.T) {
	expect := &Config{
		Repo: &RepoConfig{
			GitHubRepoOwner:       "",
			GitHubRepoName:        "",
			GitHubRemote:          "origin",
			GitHubBranch:          "main",
			GitHubHost:            "github.com",
			RequireChecks:         true,
			RequireApproval:       true,
			MergeMethod:           "rebase",
			MergeQueue:            false,
			StackMergeMode:        "tip",
			SquashGranularity:     "perStack",
			MergeWaitTimeout:      "10m",
			PRTemplateType:        "stack",
			PRTemplatePath:        "",
			PRTemplateInsertStart: "",
			PRTemplateInsertEnd:   "",
			ShowPrTitlesInStack:   false,
		},
		User: &UserConfig{
			ShowPRLink:       true,
			LogGitCommands:   false,
			LogGitHubCalls:   false,
			StatusBitsHeader: true,
			StatusBitsEmojis: true,
			BranchPrefix:     "spr",
		},
		State: &InternalState{
			MergeCheckCommit: map[string]string{},
		},
	}
	actual := DefaultConfig()
	assert.Equal(t, expect, actual)
}

func TestMergeMethodHelper(t *testing.T) {
	for _, tc := range []struct {
		configValue string
		expected    genclient.PullRequestMergeMethod
	}{
		{
			configValue: "rebase",
			expected:    genclient.PullRequestMergeMethod_REBASE,
		},
		{
			configValue: "",
			expected:    genclient.PullRequestMergeMethod_REBASE,
		},
		{
			configValue: "Merge",
			expected:    genclient.PullRequestMergeMethod_MERGE,
		},
		{
			configValue: "SQUASH",
			expected:    genclient.PullRequestMergeMethod_SQUASH,
		},
	} {
		tcName := tc.configValue
		if tcName == "" {
			tcName = "<EMPTY>"
		}
		t.Run(tcName, func(t *testing.T) {
			config := &Config{Repo: &RepoConfig{MergeMethod: tc.configValue}}
			actual, err := config.MergeMethod()
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
	t.Run("invalid", func(t *testing.T) {
		config := &Config{Repo: &RepoConfig{MergeMethod: "magic"}}
		actual, err := config.MergeMethod()
		assert.Error(t, err)
		assert.Empty(t, actual)
	})
}

func TestNormalizeConfig(t *testing.T) {
	t.Run("PRTemplatePath provided sets PRTemplateType to custom", func(t *testing.T) {
		cfg := &Config{
			Repo: &RepoConfig{
				PRTemplateType: "stack",
				PRTemplatePath: "/path/to/template.md",
			},
		}
		cfg.Normalize()
		assert.Equal(t, "custom", cfg.Repo.PRTemplateType)
		assert.Equal(t, "/path/to/template.md", cfg.Repo.PRTemplatePath)
	})

	t.Run("PRTemplatePath empty does not change PRTemplateType", func(t *testing.T) {
		cfg := &Config{
			Repo: &RepoConfig{
				PRTemplateType: "stack",
				PRTemplatePath: "",
			},
		}
		cfg.Normalize()
		assert.Equal(t, "stack", cfg.Repo.PRTemplateType)
		assert.Equal(t, "", cfg.Repo.PRTemplatePath)
	})

	t.Run("PRTemplatePath provided overrides existing PRTemplateType", func(t *testing.T) {
		cfg := &Config{
			Repo: &RepoConfig{
				PRTemplateType: "why_what",
				PRTemplatePath: "/custom/template.md",
			},
		}
		cfg.Normalize()
		assert.Equal(t, "custom", cfg.Repo.PRTemplateType)
		assert.Equal(t, "/custom/template.md", cfg.Repo.PRTemplatePath)
	})

	t.Run("DefaultConfig with PRTemplatePath sets PRTemplateType to custom", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Repo.PRTemplatePath = "/path/to/template.md"
		cfg.Normalize()
		assert.Equal(t, "custom", cfg.Repo.PRTemplateType)
	})
}

func TestMergeAllEnabled(t *testing.T) {
	cfg := EmptyConfig()
	assert.False(t, cfg.MergeAllEnabled(), "zero value must mean tip mode")
	cfg.Repo.StackMergeMode = "tip"
	assert.False(t, cfg.MergeAllEnabled())
	cfg.Repo.StackMergeMode = "mergeAll"
	assert.True(t, cfg.MergeAllEnabled())
	cfg.Repo.StackMergeMode = "MERGEALL" // case-insensitive
	assert.True(t, cfg.MergeAllEnabled())
}

func TestSquashPerPR(t *testing.T) {
	cfg := EmptyConfig()
	assert.False(t, cfg.SquashPerPR(), "zero value must mean perStack")
	cfg.Repo.SquashGranularity = "perStack"
	assert.False(t, cfg.SquashPerPR())
	cfg.Repo.SquashGranularity = "perPR"
	assert.True(t, cfg.SquashPerPR())
}

func TestMergeWaitTimeoutDuration(t *testing.T) {
	cfg := EmptyConfig()
	assert.Equal(t, 10*time.Minute, cfg.MergeWaitTimeoutDuration(), "zero value falls back to 10m")
	cfg.Repo.MergeWaitTimeout = "90s"
	assert.Equal(t, 90*time.Second, cfg.MergeWaitTimeoutDuration())
	cfg.Repo.MergeWaitTimeout = "garbage"
	assert.Equal(t, 10*time.Minute, cfg.MergeWaitTimeoutDuration())
	cfg.Repo.MergeWaitTimeout = "-5m"
	assert.Equal(t, 10*time.Minute, cfg.MergeWaitTimeoutDuration())
}

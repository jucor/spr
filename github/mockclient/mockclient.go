package mockclient

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/stretchr/testify/require"
)

const (
	NobodyUserID = "U_kgDOBb2UmA"
	NobodyLogin  = "nobody"
)

// NewMockClient creates a new mock client
func NewMockClient(t *testing.T) *MockClient {
	return &MockClient{
		assert: require.New(t),
	}
}

type MockClient struct {
	assert       *require.Assertions
	Info         *github.GitHubInfo
	InfoQueue    []*github.GitHubInfo // when non-empty, GetInfo pops from here instead of returning Info
	expect       []expectation
	expectMutex  sync.Mutex
	Synchronized bool // When true code is executed without goroutines. Allows test to be deterministic
}

func (c *MockClient) GetInfo(ctx context.Context, gitcmd git.GitInterface) *github.GitHubInfo {
	fmt.Printf("HUB: GetInfo\n")
	c.verifyExpectation(expectation{
		op: getInfoOP,
	})
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()
	if len(c.InfoQueue) > 0 {
		next := c.InfoQueue[0]
		c.InfoQueue = c.InfoQueue[1:]
		return next
	}
	return c.Info
}

func (c *MockClient) GetAssignableUsers(ctx context.Context) []github.RepoAssignee {
	fmt.Printf("HUB: GetAssignableUsers\n")
	c.verifyExpectation(expectation{
		op: getAssignableUsersOP,
	})
	return []github.RepoAssignee{
		{
			ID:    NobodyUserID,
			Login: NobodyLogin,
			Name:  "No Body",
		},
	}
}

func (c *MockClient) CreatePullRequest(ctx context.Context, gitcmd git.GitInterface, info *github.GitHubInfo,
	commit git.Commit, prevCommit *git.Commit) *github.PullRequest {
	fmt.Printf("HUB: CreatePullRequest\n")
	c.verifyExpectation(expectation{
		op:     createPullRequestOP,
		commit: commit,
		prev:   prevCommit,
	})

	// TODO - don't hardcode ID and Number
	// TODO - set FromBranch and ToBranch correctly
	return &github.PullRequest{
		ID:         "001",
		Number:     1,
		FromBranch: "from_branch",
		ToBranch:   "to_branch",
		Commit:     commit,
		Title:      commit.Subject,
		MergeStatus: github.PullRequestMergeStatus{
			ChecksPass:     github.CheckStatusPass,
			ReviewApproved: true,
			NoConflicts:    true,
			Stacked:        true,
		},
	}
}

func (c *MockClient) UpdatePullRequest(ctx context.Context, gitcmd git.GitInterface, info *github.GitHubInfo, pullRequests []*github.PullRequest, pr *github.PullRequest, commit git.Commit, prevCommit *git.Commit) {
	fmt.Printf("HUB: UpdatePullRequest\n")
	c.verifyExpectation(expectation{
		op:     updatePullRequestOP,
		commit: commit,
		prev:   prevCommit,
	})
}

func (c *MockClient) AddReviewers(ctx context.Context, pr *github.PullRequest, userIDs []string) {
	c.verifyExpectation(expectation{
		op:      addReviewersOP,
		userIDs: userIDs,
	})
}

func (c *MockClient) CommentPullRequest(ctx context.Context, pr *github.PullRequest, comment string) {
	fmt.Printf("HUB: CommentPullRequest\n")
	c.verifyExpectation(expectation{
		op:     commentPullRequestOP,
		commit: pr.Commit,
	})
}

func (c *MockClient) MergePullRequest(ctx context.Context,
	pr *github.PullRequest, mergeMethod genclient.PullRequestMergeMethod, msg *github.MergeCommitMessage) {
	fmt.Printf("HUB: MergePullRequest, method=%q\n", mergeMethod)
	var headline string
	if msg != nil {
		headline = msg.Headline
	}
	c.verifyExpectation(expectation{
		op:          mergePullRequestOP,
		commit:      pr.Commit,
		mergeMethod: mergeMethod,
		msgHeadline: headline,
	})
}

func (c *MockClient) MergePullRequestDirect(ctx context.Context,
	pr *github.PullRequest, mergeMethod genclient.PullRequestMergeMethod, msg *github.MergeCommitMessage) {
	fmt.Printf("HUB: MergePullRequestDirect, method=%q\n", mergeMethod)
	var headline string
	if msg != nil {
		headline = msg.Headline
	}
	c.verifyExpectation(expectation{
		op:          mergePullRequestDirectOP,
		commit:      pr.Commit,
		mergeMethod: mergeMethod,
		msgHeadline: headline,
	})
}

func (c *MockClient) ClosePullRequest(ctx context.Context, pr *github.PullRequest) {
	fmt.Printf("HUB: ClosePullRequest\n")
	c.verifyExpectation(expectation{
		op:     closePullRequestOP,
		commit: pr.Commit,
	})
}

func (c *MockClient) ExpectGetInfo() {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op: getInfoOP,
	})
}

func (c *MockClient) ExpectGetAssignableUsers() {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op: getAssignableUsersOP,
	})
}

func (c *MockClient) ExpectCreatePullRequest(commit git.Commit, prev *git.Commit) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:     createPullRequestOP,
		commit: commit,
		prev:   prev,
	})
}

func (c *MockClient) ExpectUpdatePullRequest(commit git.Commit, prev *git.Commit) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:     updatePullRequestOP,
		commit: commit,
		prev:   prev,
	})
}

func (c *MockClient) ExpectAddReviewers(userIDs []string) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:      addReviewersOP,
		userIDs: userIDs,
	})
}

func (c *MockClient) ExpectCommentPullRequest(commit git.Commit) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:     commentPullRequestOP,
		commit: commit,
	})
}

func (c *MockClient) ExpectMergePullRequest(commit git.Commit, mergeMethod genclient.PullRequestMergeMethod) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:          mergePullRequestOP,
		commit:      commit,
		mergeMethod: mergeMethod,
	})
}

func (c *MockClient) ExpectMergePullRequestWithMessage(commit git.Commit,
	mergeMethod genclient.PullRequestMergeMethod, msgHeadline string) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()
	c.expect = append(c.expect, expectation{
		op:          mergePullRequestOP,
		commit:      commit,
		mergeMethod: mergeMethod,
		msgHeadline: msgHeadline,
	})
}

func (c *MockClient) ExpectMergePullRequestDirect(commit git.Commit,
	mergeMethod genclient.PullRequestMergeMethod, msgHeadline string) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()
	c.expect = append(c.expect, expectation{
		op:          mergePullRequestDirectOP,
		commit:      commit,
		mergeMethod: mergeMethod,
		msgHeadline: msgHeadline,
	})
}

func (c *MockClient) ExpectClosePullRequest(commit git.Commit) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	c.expect = append(c.expect, expectation{
		op:     closePullRequestOP,
		commit: commit,
	})
}

func (c *MockClient) verifyExpectation(actual expectation) {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	if !c.Synchronized {
		for i := 0; i != len(c.expect); i++ {
			if reflect.DeepEqual(c.expect[i], actual) {
				// Zero out the expectation once it has been met
				c.expect[i] = expectation{}
				return
			}
		}

		c.assert.FailNowf("verifyExpectations", "Unexpected github command: %v\n", actual)
	} else {
		c.assert.NotEmpty(c.expect, fmt.Sprintf("Unexpected github command %v\n", actual))

		expected := c.expect[0]
		c.assert.True(reflect.DeepEqual(expected, actual), "Expected github command %v got %v", expected, actual)
		c.expect = c.expect[1:]
	}
}

func (c *MockClient) ExpectationsMet() {
	c.expectMutex.Lock()
	defer c.expectMutex.Unlock()

	// Note that above the expectations are zeroed (not removed)
	for _, expected := range c.expect {
		if expected.op != "" {
			c.assert.FailNowf("ExpectationsMet", "expected additional github commands: %#v", expected)
		}
	}
}

type operation string

const (
	getInfoOP                operation = "GetInfo"
	getAssignableUsersOP     operation = "GetAssignableUsers"
	createPullRequestOP      operation = "CreatePullRequest"
	updatePullRequestOP      operation = "UpdatePullRequest"
	addReviewersOP           operation = "AddReviewers"
	commentPullRequestOP     operation = "CommentPullRequest"
	mergePullRequestOP       operation = "MergePullRequest"
	mergePullRequestDirectOP operation = "MergePullRequestDirect"
	closePullRequestOP       operation = "ClosePullRequest"
)

type expectation struct {
	op          operation
	commit      git.Commit
	prev        *git.Commit
	mergeMethod genclient.PullRequestMergeMethod
	msgHeadline string
	userIDs     []string
}

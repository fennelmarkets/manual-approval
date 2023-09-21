package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/actions-go/toolkit/core"
	"github.com/actions-go/toolkit/github"
	gh "github.com/google/go-github/v43/github"
)

type approvalEnvironment struct {
	client              *gh.Client
	repo                github.ActionRepo
	runID               int
	approvalIssue       *gh.Issue
	approvalIssueNumber int
	issueTitle          string
	issueBody           string
	issueLabels         []string
	issueApprovers      []string
	minimumApprovals    int
}

func newApprovalEnvironment(client *gh.Client, repo github.ActionRepo, runID int, approvers []string, minimumApprovals int, issueTitle, issueBody string, issueLabels []string) (*approvalEnvironment, error) {
	return &approvalEnvironment{
		client:           client,
		repo:             repo,
		runID:            runID,
		issueApprovers:   approvers,
		minimumApprovals: minimumApprovals,
		issueTitle:       issueTitle,
		issueBody:        issueBody,
		issueLabels:      issueLabels,
	}, nil
}

func (a approvalEnvironment) runURL() string {
	return fmt.Sprintf("%s%s/%s/actions/runs/%d", a.client.BaseURL.String(), a.repo.Owner, a.repo.Repo, a.runID)
}

func (a *approvalEnvironment) createApprovalIssue(ctx context.Context) error {
	issueTitle := "Approval required"

	if a.issueTitle != "" {
		issueTitle = fmt.Sprintf("%s: %s", issueTitle, a.issueTitle)
	}

	issueBody := fmt.Sprintf(`Workflow ([#%d](%s)) is pending manual review.

To continue the workflow, respond with one of the following:
%s

To cancel the workflow, respond with one of the following:
%s`,
		a.runID,
		a.runURL(),
		formatAcceptedWords(approvedWords),
		formatAcceptedWords(deniedWords),
	)

	if a.issueBody != "" {
		issueBody = fmt.Sprintf("%s\n\n%s", a.issueBody, issueBody)
	}

	var err error
	core.Infof(
		"Creating issue in repo %s/%s with the following content:\nTitle: %s\nApprovers: %s\nBody:\n%s",
		a.repo.Owner,
		a.repo.Repo,
		issueTitle,
		a.issueApprovers,
		issueBody,
	)
	a.approvalIssue, _, err = a.client.Issues.Create(ctx, a.repo.Owner, a.repo.Repo, &gh.IssueRequest{
		Title:     &issueTitle,
		Body:      &issueBody,
		Assignees: &a.issueApprovers,
		Labels:    &a.issueLabels,
	})
	if err != nil {
		return err
	}
	a.approvalIssueNumber = a.approvalIssue.GetNumber()

	core.Debugf("Issue created: %s\n", a.approvalIssue.GetURL())
	return nil
}

func approvalFromComments(comments []*gh.IssueComment, approvers []string, minimumApprovals int) (approvalStatus, error) {
	remainingApprovers := make([]string, len(approvers))
	copy(remainingApprovers, approvers)

	if minimumApprovals == 0 {
		minimumApprovals = len(approvers)
	}

	for _, comment := range comments {
		commentUser := comment.User.GetLogin()
		approverIdx := approversIndex(remainingApprovers, commentUser)
		if approverIdx < 0 {
			continue
		}

		commentBody := comment.GetBody()
		isApprovalComment, err := isApproved(commentBody)
		if err != nil {
			return approvalStatusPending, err
		}
		if isApprovalComment {
			if len(remainingApprovers) == len(approvers)-minimumApprovals+1 {
				return approvalStatusApproved, nil
			}
			remainingApprovers[approverIdx] = remainingApprovers[len(remainingApprovers)-1]
			remainingApprovers = remainingApprovers[:len(remainingApprovers)-1]
			continue
		}

		isDenialComment, err := isDenied(commentBody)
		if err != nil {
			return approvalStatusPending, err
		}
		if isDenialComment {
			return approvalStatusDenied, nil
		}
	}

	return approvalStatusPending, nil
}

func approversIndex(approvers []string, name string) int {
	for idx, approver := range approvers {
		if approver == name {
			return idx
		}
	}
	return -1
}

func isApproved(commentBody string) (bool, error) {
	for _, approvedWord := range approvedWords {
		re, err := regexp.Compile(fmt.Sprintf("(?i)^%s[.!]*\n*\\s*$", approvedWord))
		if err != nil {
			core.SetFailedf("Error parsing. %v", err)
			return false, err
		}

		matched := re.MatchString(commentBody)

		if matched {
			return true, nil
		}
	}

	return false, nil
}

func isDenied(commentBody string) (bool, error) {
	for _, deniedWord := range deniedWords {
		re, err := regexp.Compile(fmt.Sprintf("(?i)^%s[.!]*\n*\\s*$", deniedWord))
		if err != nil {
			core.SetFailedf("Error parsing. %v", err)
			return false, err
		}
		matched := re.MatchString(commentBody)
		if matched {
			return true, nil
		}
	}

	return false, nil
}

func formatAcceptedWords(words []string) string {
	var quotedWords []string

	for _, word := range words {
		quotedWords = append(quotedWords, fmt.Sprintf("* %s", word))
	}

	return strings.Join(quotedWords, "\n")
}

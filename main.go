package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/actions-go/toolkit/core"
	"github.com/actions-go/toolkit/github"
	gh "github.com/google/go-github/v43/github"
	"golang.org/x/oauth2"
)

func handleInterrupt(ctx context.Context, client *gh.Client, apprv *approvalEnvironment) {
	newState := "closed"
	closeComment := "Workflow cancelled, closing issue."
	core.Warning(closeComment)
	_, _, err := client.Issues.CreateComment(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueComment{
		Body: &closeComment,
	})
	if err != nil {
		core.SetFailedf("error commenting on issue: %v", err)
		return
	}
	_, _, err = client.Issues.Edit(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueRequest{State: &newState})
	if err != nil {
		core.SetFailedf("error closing issue: %v", err)
		return
	}
}

func newCommentLoopChannel(ctx context.Context, apprv *approvalEnvironment, client *gh.Client) chan int {
	channel := make(chan int)
	go func() {
		for {
			comments, _, err := client.Issues.ListComments(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueListCommentsOptions{})
			if err != nil {
				core.SetFailedf("error getting comments: %v", err)
				channel <- 1
				close(channel)
			}

			approved, err := approvalFromComments(comments, apprv.issueApprovers, apprv.minimumApprovals)
			if err != nil {
				core.SetFailedf("error getting approval from comments: %v", err)
				channel <- 1
				close(channel)
			}
			core.Debugf("Workflow status: %s", approved)
			switch approved {
			case approvalStatusApproved:
				newState := "closed"
				closeComment := "Approval has been granted, continuing workflow and closing this issue."
				_, _, err := client.Issues.CreateComment(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueComment{
					Body: &closeComment,
				})
				if err != nil {
					core.SetFailedf("error commenting on issue: %v", err)
					channel <- 1
					close(channel)
				}
				_, _, err = client.Issues.Edit(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueRequest{State: &newState})
				if err != nil {
					core.SetFailedf("error closing issue: %v", err)
					channel <- 1
					close(channel)
				}
				core.Info("Workflow manual approval completed")
				channel <- 0
				close(channel)
			case approvalStatusDenied:
				newState := "closed"
				closeComment := "Approval was denied. Closing issue and failing workflow."
				_, _, err := client.Issues.CreateComment(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueComment{
					Body: &closeComment,
				})
				if err != nil {
					core.SetFailedf("error commenting on issue: %v", err)
					channel <- 1
					close(channel)
				}
				_, _, err = client.Issues.Edit(ctx, apprv.repo.Owner, apprv.repo.Repo, apprv.approvalIssueNumber, &gh.IssueRequest{State: &newState})
				if err != nil {
					core.SetFailedf("error closing issue: %v", err)
					channel <- 1
					close(channel)
				}
				core.SetFailed("Workflow manual approval denied")
				channel <- 1
				close(channel)
			}

			time.Sleep(pollingInterval)
		}
	}()
	return channel
}

func newGithubClient(ctx context.Context) (*gh.Client, error) {
	token, ok := core.GetInput(inputGithubToken)
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	if !ok {
		return gh.NewClient(tc), fmt.Errorf("missing required input: %s", inputGithubToken)
	}

	serverUrl, serverUrlPresent := os.LookupEnv("GITHUB_SERVER_URL")
	apiUrl, apiUrlPresent := os.LookupEnv("GITHUB_API_URL")

	if serverUrlPresent {
		if !apiUrlPresent {
			apiUrl = serverUrl
		}
		return gh.NewEnterpriseClient(apiUrl, serverUrl, tc)
	}
	return gh.NewClient(tc), nil
}

func validateInput() error {
	missingEnvVars := []string{}

	if os.Getenv(envVarRunID) == "" {
		missingEnvVars = append(missingEnvVars, envVarRunID)
	}

	if _, ok := core.GetInput(inputGithubToken); !ok {
		missingEnvVars = append(missingEnvVars, inputGithubToken)
	}

	if _, ok := core.GetInput(inputApprovers); !ok {
		missingEnvVars = append(missingEnvVars, inputApprovers)
	}

	if len(missingEnvVars) > 0 {
		return fmt.Errorf("missing inputs vars: %v", missingEnvVars)
	}
	return nil
}

func main() {
	if err := validateInput(); err != nil {
		core.SetFailedf("%v", err)
		os.Exit(1)
	}

	actionCtx := github.ParseActionEnv()

	runID, err := strconv.Atoi(os.Getenv(envVarRunID))
	if err != nil {
		core.SetFailedf("error getting runID: %v", err)
		os.Exit(1)
	}
	repoOwner := actionCtx.Repo.Owner

	ctx := context.Background()
	client, err := newGithubClient(ctx)
	if err != nil {
		core.SetFailedf("error connecting to server: %v", err)
		os.Exit(1)
	}

	approvers, err := retrieveApprovers(client, repoOwner)
	if err != nil {
		core.SetFailedf("error retrieving approvers: %v", err)
		os.Exit(1)
	}

	issueTitle := core.GetInputOrDefault(inputIssueTitle, "")
	issueBody := core.GetInputOrDefault(inputIssueBody, "")
	issueLabelsRaw := core.GetInputOrDefault(inputLabels, "")
	issueLabels := strings.Split(issueLabelsRaw, ",")
	for i := range issueLabels {
		issueLabels[i] = strings.TrimSpace(issueLabels[i])
	}
	minimumApprovalsRaw := core.GetInputOrDefault(inputMinimumApprovals, "")
	minimumApprovals := 0
	if minimumApprovalsRaw != "" {
		minimumApprovals, err = strconv.Atoi(minimumApprovalsRaw)
		if err != nil {
			core.SetFailedf("error parsing minimum approvals: %v", err)
			os.Exit(1)
		}
	}
	apprv, err := newApprovalEnvironment(client, actionCtx.Repo, runID, approvers, minimumApprovals, issueTitle, issueBody, issueLabels)
	if err != nil {
		core.SetFailedf("error creating approval environment: %v", err)
		os.Exit(1)
	}

	err = apprv.createApprovalIssue(ctx)
	if err != nil {
		core.SetFailedf("error creating issue: %v", err)
		os.Exit(1)
	}

	start := time.Now()

	killSignalChannel := make(chan os.Signal, 1)
	signal.Notify(killSignalChannel, os.Interrupt)

	commentLoopChannel := newCommentLoopChannel(ctx, apprv, client)

	select {
	case exitCode := <-commentLoopChannel:
		core.SetOutput(outputDuration, fmt.Sprintf("%v", time.Since(start).Minutes()))
		os.Exit(exitCode)
	case <-killSignalChannel:
		core.SetOutput(outputDuration, fmt.Sprintf("%v", time.Since(start).Minutes()))
		handleInterrupt(ctx, client, apprv)
		os.Exit(1)
	}
}

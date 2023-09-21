package main

import (
	"strings"
	"time"

	"github.com/actions-go/toolkit/core"
)

const (
	pollingInterval time.Duration = 10 * time.Second

	envVarRunID string = "GITHUB_RUN_ID"
	//
	inputGithubToken                        string = "secret"
	inputApprovers                          string = "approvers"
	inputMinimumApprovals                   string = "minimum-approvals"
	inputIssueTitle                         string = "issue-title"
	inputIssueBody                          string = "issue-body"
	inputExcludeWorkflowInitiatorAsApprover string = "exclude-workflow-initiator-as-approver"
	inputAdditionalApprovalWords            string = "additional-approved-words"
	inputAdditionalDeniedWords              string = "additional-denied-words"
	inputLabels                             string = "labels"
	outputDuration                          string = "duration"
)

var (
	additionalApprovedWords = readAdditionalWords(inputAdditionalApprovalWords)
	additionalDeniedWords   = readAdditionalWords(inputAdditionalDeniedWords)

	approvedWords = append([]string{"approved", "yes", "👍", "✅", "🚀", "🚢"}, additionalApprovedWords...)
	deniedWords   = append([]string{"denied", "no", "👎", "❌", "𝕏", "🚫"}, additionalDeniedWords...)
)

func readAdditionalWords(envVar string) []string {
	rawValue := strings.TrimSpace(core.GetInputOrDefault(envVar, ""))
	if len(rawValue) == 0 {
		// Nothing else to do here.
		return []string{}
	}
	slicedWords := strings.Split(rawValue, ",")
	for i := range slicedWords {
		// no leading or trailing spaces in user provided words.
		slicedWords[i] = strings.TrimSpace(slicedWords[i])
	}
	return slicedWords
}

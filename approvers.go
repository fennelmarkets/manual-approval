package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/actions-go/toolkit/core"
	"github.com/actions-go/toolkit/github"
	gh "github.com/google/go-github/v43/github"
)

func retrieveApprovers(client *gh.Client, repoOwner string) ([]string, error) {
	workflowInitiator := github.ParseActionEnv().Actor
	shouldExcludeWorkflowInitiator := core.GetBoolInput(inputExcludeWorkflowInitiatorAsApprover)

	approvers := []string{}
	requiredApproversRaw := core.GetInputOrDefault(inputApprovers, "")
	requiredApprovers := strings.Split(requiredApproversRaw, ",")

	for i := range requiredApprovers {
		requiredApprovers[i] = strings.TrimSpace(requiredApprovers[i])
	}

	for _, approverUser := range requiredApprovers {
		expandedUsers := expandGroupFromUser(client, repoOwner, approverUser, workflowInitiator, shouldExcludeWorkflowInitiator)
		if expandedUsers != nil {
			approvers = append(approvers, expandedUsers...)
		} else if strings.EqualFold(workflowInitiator, approverUser) && shouldExcludeWorkflowInitiator {
			core.Debugf("Not adding user '%s' as an approver as they are the workflow initiator", approverUser)
		} else {
			approvers = append(approvers, approverUser)
		}
	}

	approvers = deduplicateUsers(approvers)

	minimumApprovalsRaw := core.GetInputOrDefault(inputMinimumApprovals, "")
	minimumApprovals := len(approvers)
	var err error
	if minimumApprovalsRaw != "" {
		minimumApprovals, err = strconv.Atoi(minimumApprovalsRaw)
		if err != nil {
			return nil, fmt.Errorf("error parsing minimum number of approvals: %w", err)
		}
	}

	if minimumApprovals > len(approvers) {
		return nil, fmt.Errorf("error: minimum required approvals (%d) is greater than the total number of approvers (%d)", minimumApprovals, len(approvers))
	}

	return approvers, nil
}

func expandGroupFromUser(client *gh.Client, org, userOrTeam string, workflowInitiator string, shouldExcludeWorkflowInitiator bool) []string {
	core.Debugf("Attempting to expand user %s/%s as a group (may not succeed)", org, userOrTeam)

	// GitHub replaces periods in the team name with hyphens. If a period is
	// passed to the request it would result in a 404. So we need to replace
	// and occurrences with a hyphen.
	formattedUserOrTeam := strings.ReplaceAll(userOrTeam, ".", "-")

	users, _, err := client.Teams.ListTeamMembersBySlug(context.Background(), org, formattedUserOrTeam, &gh.TeamListTeamMembersOptions{})
	if err != nil {
		core.SetFailedf("%v", err)
		return nil
	}

	userNames := make([]string, 0, len(users))
	for _, user := range users {
		userName := user.GetLogin()
		if strings.EqualFold(userName, workflowInitiator) && shouldExcludeWorkflowInitiator {
			core.Debugf("Not adding user '%s' from group '%s' as an approver as they are the workflow initiator", userName, userOrTeam)
		} else {
			userNames = append(userNames, userName)
		}
	}

	return userNames
}

func deduplicateUsers(users []string) []string {
	uniqValuesByKey := make(map[string]bool)
	uniqUsers := []string{}
	for _, user := range users {
		if _, ok := uniqValuesByKey[user]; !ok {
			uniqValuesByKey[user] = true
			uniqUsers = append(uniqUsers, user)
		}
	}
	return uniqUsers
}

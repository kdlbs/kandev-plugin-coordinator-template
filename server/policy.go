package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

type coordinatorPolicyStatus struct {
	ActiveRuns         int     `json:"activeRuns"`
	MaxConcurrentRuns  int     `json:"maxConcurrentRuns"`
	EstimatedBudget    float64 `json:"estimatedBudgetUsd"`
	UsageUSD           float64 `json:"usageUsd"`
	UsageState         string  `json:"usageState"`
	PotentialOvershoot bool    `json:"potentialOvershoot"`
	Blocked            bool    `json:"blocked"`
	Reason             string  `json:"reason,omitempty"`
	Error              string  `json:"error,omitempty"`
}

func (p *coordinatorPlugin) coordinatorPolicy(ctx context.Context, workspaceID string, instance coordinatorInstance) (coordinatorPolicyStatus, error) {
	maxRuns := instance.MaxConcurrentRuns
	if maxRuns < 1 {
		maxRuns = 1
	}
	result := coordinatorPolicyStatus{
		MaxConcurrentRuns: maxRuns,
		EstimatedBudget:   instance.EstimatedBudgetUSD,
		UsageState:        "unknown",
	}
	if len(instance.TaskIDs) == 0 {
		return result, nil
	}
	host := p.Host()
	queries, ok := pluginsdk.HostExactQueries(host)
	if !ok {
		return result, fmt.Errorf("exact task observations are unavailable")
	}
	if _, _, unavailable := p.exactCapability(ctx, host, workspaceID, "GetTaskExact"); unavailable != nil {
		return result, fmt.Errorf("exact task observations are not authorized")
	}
	for _, taskID := range instance.TaskIDs {
		observation, _, err := queries.GetTask(ctx, pluginsdk.ExactTaskGetQuery{
			RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskID: taskID,
		})
		if err != nil || !observation.StatusKnown {
			return result, fmt.Errorf("task %s status is unknown", taskID)
		}
		if strings.EqualFold(observation.ExecutionState, "starting") || strings.EqualFold(observation.ExecutionState, "running") {
			result.ActiveRuns++
		}
	}
	if result.ActiveRuns >= result.MaxConcurrentRuns {
		result.Blocked = true
		result.Reason = "concurrency_limit"
	}
	if instance.EstimatedBudgetUSD <= 0 {
		return result, nil
	}
	if _, _, unavailable := p.exactCapability(ctx, host, workspaceID, "ListTaskUsageExact"); unavailable != nil {
		return result, fmt.Errorf("task usage is not authorized")
	}
	page, err := queries.ListTaskUsage(ctx, pluginsdk.ExactTaskUsageQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskIDs: append([]string(nil), instance.TaskIDs...),
		Page: pluginsdk.ExactReadPage{Limit: 200},
	})
	if err != nil || page.PageInfo.HasMore {
		return result, fmt.Errorf("task usage is incomplete")
	}
	if len(page.Items) == 0 {
		return result, nil
	}
	complete := true
	known := true
	for _, usage := range page.Items {
		if usage.Currency != "USD" || usage.CostSubcents == nil {
			known = false
			continue
		}
		result.UsageUSD += float64(*usage.CostSubcents) / 10000
		if !usage.CostComplete || usage.CostUnknownReason != nil {
			complete = false
		}
	}
	if !known {
		result.UsageState = "unknown"
	} else if complete {
		result.UsageState = "measured"
	} else {
		result.UsageState = "estimated"
	}
	if result.UsageState != "unknown" && result.UsageUSD >= instance.EstimatedBudgetUSD && !result.Blocked {
		result.Blocked = true
		result.Reason = "budget_limit"
	}
	result.PotentialOvershoot = result.ActiveRuns > 0 && result.UsageState != "measured"
	return result, nil
}

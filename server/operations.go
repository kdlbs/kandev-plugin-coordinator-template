package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

type taskAdoptRequest struct {
	InstanceKey    string `json:"instance_key"`
	TaskID         string `json:"task_id"`
	RequestID      string `json:"request_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Reason         string `json:"reason"`
}

type completionCriterionRequest struct {
	ID               string `json:"id"`
	Description      string `json:"description"`
	EvidenceKind     string `json:"evidence_kind"`
	EvidenceID       string `json:"evidence_id"`
	EvidenceRevision string `json:"evidence_revision"`
}

type completionCriteriaRequest struct {
	InstanceKey string                       `json:"instance_key"`
	TaskID      string                       `json:"task_id"`
	RequestID   string                       `json:"request_id"`
	Criteria    []completionCriterionRequest `json:"criteria"`
}

type completionEvidenceRequest struct {
	InstanceKey      string `json:"instance_key"`
	TaskID           string `json:"task_id"`
	CriterionID      string `json:"criterion_id"`
	RequestID        string `json:"request_id"`
	EvidenceKind     string `json:"evidence_kind"`
	EvidenceID       string `json:"evidence_id"`
	EvidenceRevision string `json:"evidence_revision"`
	Summary          string `json:"summary"`
	Reference        string `json:"reference"`
}

type issueWriteRequest struct {
	InstanceKey    string `json:"instance_key"`
	TaskID         string `json:"task_id"`
	RequestID      string `json:"request_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Body           string `json:"body"`
	TargetID       string `json:"target_id"`
}

func (p *coordinatorPlugin) adoptTask(ctx context.Context, workspaceID string, input taskAdoptRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if instance.Paused || input.TaskID == "" || input.RequestID == "" {
		return actionError(http.StatusBadRequest, "active instance, task, and stable request identity are required"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	if current, err := store.GetTaskClaim(ctx, workspaceID, instance.Key, input.TaskID); err == nil && current.Generation > 0 {
		return actionError(http.StatusConflict, "this coordinator instance already holds the task claim"), nil
	} else if err != nil && err != sql.ErrNoRows {
		return actionError(http.StatusInternalServerError, "task claim state is unavailable"), nil
	}
	observation, err := p.readTaskObservation(ctx, workspaceID, input.TaskID)
	if err != nil || !observation.StatusKnown {
		return actionError(http.StatusServiceUnavailable, "canonical task status is unavailable"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "AcquireTaskManagementClaimExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostTaskManagementClaims(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "task management claims are unavailable"), nil
	}
	requestID, idempotencyKey := defaultIDs(input.RequestID, input.IdempotencyKey)
	command, claim, err := manager.Acquire(ctx, pluginsdk.ExactTaskManagementClaimAcquire{
		ExactTaskManagementClaimCommand: pluginsdk.ExactTaskManagementClaimCommand{
			RequestID: requestID, WorkspaceID: workspaceID, TaskID: input.TaskID,
			ExpectedTaskResourceVersion: observation.ResourceVersion, IdempotencyKey: idempotencyKey,
			Reason: strings.TrimSpace(input.Reason), ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		},
		InstanceKey: instance.Key,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not acquire the task claim"), nil
	}
	if !commandSucceeded(command) || claim == nil {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	if err := store.SaveTaskClaim(ctx, storedTaskClaim{WorkspaceID: workspaceID, InstanceKey: instance.Key,
		TaskID: input.TaskID, Generation: claim.Generation, ResourceVersion: claim.ResourceVersion}); err != nil {
		return actionError(http.StatusInternalServerError, "claim was acquired but its coordinator receipt could not be saved"), nil
	}
	p.mu.Lock()
	state, err := p.readState(ctx, workspaceID)
	if err == nil {
		index := findInstance(state.Instances, instance.Key)
		if index >= 0 {
			state.Instances[index].TaskIDs = appendUnique(state.Instances[index].TaskIDs, input.TaskID)
			err = p.writeState(ctx, workspaceID, state)
		}
	}
	p.mu.Unlock()
	if err != nil {
		return actionError(http.StatusInternalServerError, "claim was acquired but the task link could not be saved"), nil
	}
	if err := p.installTaskWatch(ctx, workspaceID, instance.Key, input.TaskID, 0); err != nil {
		return actionError(http.StatusServiceUnavailable, "claim was acquired but its task watch could not be saved"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "claim": claim})
}

func (p *coordinatorPlugin) releaseTask(ctx context.Context, workspaceID string, input taskAdoptRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if input.TaskID == "" || input.RequestID == "" {
		return actionError(http.StatusBadRequest, "task and stable request identity are required"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	claim, err := store.GetTaskClaim(ctx, workspaceID, instance.Key, input.TaskID)
	if err != nil {
		return actionError(http.StatusConflict, "this coordinator instance does not hold the task claim"), nil
	}
	observation, err := p.readTaskObservation(ctx, workspaceID, input.TaskID)
	if err != nil {
		return actionError(http.StatusServiceUnavailable, "canonical task status is unavailable"), nil
	}
	_, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "ReleaseTaskManagementClaimExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostTaskManagementClaims(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "task management claims are unavailable"), nil
	}
	requestID, idempotencyKey := defaultIDs(input.RequestID, input.IdempotencyKey)
	command, released, err := manager.Release(ctx, pluginsdk.ExactTaskManagementClaimRelease{
		ExactTaskManagementClaimCommand: pluginsdk.ExactTaskManagementClaimCommand{
			RequestID: requestID, WorkspaceID: workspaceID, TaskID: input.TaskID,
			ExpectedTaskResourceVersion: observation.ResourceVersion, ExpectedClaimResourceVersion: claim.ResourceVersion,
			IdempotencyKey: idempotencyKey, Reason: strings.TrimSpace(input.Reason),
			ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		},
		InstanceKey: instance.Key,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not release the task claim"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	if err := store.DeleteTaskClaim(ctx, workspaceID, instance.Key, input.TaskID, claim.Generation); err != nil {
		return actionError(http.StatusInternalServerError, "claim was released but its local receipt could not be updated"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "claim": released})
}

func (p *coordinatorPlugin) setCompletionCriteria(ctx context.Context, workspaceID string, input completionCriteriaRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, store, claim, observation, capability, manager, result := p.completionCommandContext(ctx, workspaceID, input.InstanceKey, input.TaskID, "SetTaskCompletionCriteriaExact")
	if result != nil {
		return result, nil
	}
	if input.RequestID == "" || len(input.Criteria) == 0 || len(input.Criteria) > 20 {
		return actionError(http.StatusBadRequest, "criteria need a stable request identity and between 1 and 20 entries"), nil
	}
	criteria := make([]pluginsdk.TaskCompletionCriterionInput, 0, len(input.Criteria))
	for _, criterion := range input.Criteria {
		if strings.TrimSpace(criterion.ID) == "" || len(criterion.ID) > 128 || strings.TrimSpace(criterion.Description) == "" || len(criterion.Description) > 1000 || criterion.EvidenceKind == "" || criterion.EvidenceID == "" || criterion.EvidenceRevision == "" {
			return actionError(http.StatusBadRequest, "every criterion needs a bounded id, description, and versioned evidence subject"), nil
		}
		criteria = append(criteria, pluginsdk.TaskCompletionCriterionInput{
			ID: criterion.ID, Description: criterion.Description,
			EvidenceSubject: pluginsdk.TaskCompletionEvidenceSubject{Kind: criterion.EvidenceKind, ID: criterion.EvidenceID, Revision: criterion.EvidenceRevision},
		})
	}
	revision, err := store.GetCompletionGateRevision(ctx, workspaceID, instance.Key, input.TaskID)
	if err != nil {
		return actionError(http.StatusInternalServerError, "completion gate revision is unavailable"), nil
	}
	requestID := input.RequestID
	command, gate, err := manager.SetCriteria(ctx, pluginsdk.ExactTaskCompletionCriteria{
		RequestID: requestID, WorkspaceID: workspaceID, TaskID: input.TaskID, IdempotencyKey: requestID,
		ExpectedTaskResourceVersion: observation.ResourceVersion, ExpectedRevision: revision,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		ManagementInstanceKey: instance.Key, ExpectedClaimGeneration: claim.Generation, Criteria: criteria,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not update completion criteria"), nil
	}
	if !commandSucceeded(command) || gate == nil {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	if err := store.SaveCompletionGateRevision(ctx, workspaceID, instance.Key, input.TaskID, gate.Revision); err != nil {
		return actionError(http.StatusInternalServerError, "criteria were saved but the revision receipt could not be stored"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "gate": gate})
}

func (p *coordinatorPlugin) submitCompletionEvidence(ctx context.Context, workspaceID string, input completionEvidenceRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, store, claim, observation, capability, manager, result := p.completionCommandContext(ctx, workspaceID, input.InstanceKey, input.TaskID, "VerifyTaskCompletionCriterionExact")
	if result != nil {
		return result, nil
	}
	if input.RequestID == "" || input.CriterionID == "" || input.EvidenceKind == "" || input.EvidenceID == "" || input.EvidenceRevision == "" || strings.TrimSpace(input.Summary) == "" || len(input.Summary) > 2000 || len(input.Reference) > 2000 {
		return actionError(http.StatusBadRequest, "evidence needs a criterion, stable identity, versioned subject, and bounded summary"), nil
	}
	revision, err := store.GetCompletionGateRevision(ctx, workspaceID, instance.Key, input.TaskID)
	if err != nil || revision == 0 {
		return actionError(http.StatusConflict, "completion criteria are not available for this task"), nil
	}
	requestID := input.RequestID
	command, gate, err := manager.Verify(ctx, pluginsdk.ExactTaskCompletionEvidence{
		RequestID: requestID, WorkspaceID: workspaceID, TaskID: input.TaskID, CriterionID: input.CriterionID,
		IdempotencyKey: requestID, ExpectedTaskResourceVersion: observation.ResourceVersion, ExpectedRevision: revision,
		Evidence: pluginsdk.TaskCompletionEvidence{
			Subject: pluginsdk.TaskCompletionEvidenceSubject{Kind: input.EvidenceKind, ID: input.EvidenceID, Revision: input.EvidenceRevision},
			Summary: input.Summary, Reference: input.Reference,
		},
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		ManagementInstanceKey: instance.Key, ExpectedClaimGeneration: claim.Generation,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not submit completion evidence"), nil
	}
	if !commandSucceeded(command) || gate == nil {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	if err := store.SaveCompletionGateRevision(ctx, workspaceID, instance.Key, input.TaskID, gate.Revision); err != nil {
		return actionError(http.StatusInternalServerError, "evidence was saved but the gate revision receipt could not be stored"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "gate": gate})
}

func (p *coordinatorPlugin) completionCommandContext(ctx context.Context, workspaceID, instanceKey, taskID, method string) (coordinatorInstance, *policyStore, storedTaskClaim, pluginsdk.ExactTaskObservation, *pluginsdk.CapabilityContext, pluginsdk.ExactTaskCompletionGateCommandManager, *pluginsdk.PluginActionResponse) {
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, actionError(http.StatusNotFound, "coordinator instance not found")
	}
	store, err := p.policyStore()
	if err != nil {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable")
	}
	claim, err := store.GetTaskClaim(ctx, workspaceID, instance.Key, taskID)
	if err != nil {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, actionError(http.StatusForbidden, "explicit task adoption is required before completion operations")
	}
	observation, err := p.readTaskObservation(ctx, workspaceID, taskID)
	if err != nil {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, actionError(http.StatusServiceUnavailable, "canonical task status is unavailable")
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, method)
	if result != nil {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, result
	}
	manager, ok := pluginsdk.HostTaskCompletionGates(exact)
	if !ok {
		return coordinatorInstance{}, nil, storedTaskClaim{}, pluginsdk.ExactTaskObservation{}, nil, nil, actionError(http.StatusNotImplemented, "task completion gates are unavailable")
	}
	return instance, store, claim, observation, capability, manager, nil
}

func (p *coordinatorPlugin) issueCapabilities(ctx context.Context, workspaceID, taskID string) (*pluginsdk.PluginActionResponse, error) {
	if taskID == "" {
		return actionError(http.StatusBadRequest, "task id is required"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "GetSourceIssueCapabilitiesExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostSourceIssueWriteback(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "linked issue operations are unavailable"), nil
	}
	command, caps, receipt, err := manager.GetCapabilities(ctx, pluginsdk.SourceIssueCapabilitiesQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskID: taskID,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not read linked issue capabilities"), nil
	}
	if !commandSucceeded(command) || caps == nil {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	transitions := make([]map[string]string, 0, len(caps.Transitions))
	for _, transition := range caps.Transitions {
		transitions = append(transitions, map[string]string{"targetId": transition.TargetID, "targetName": transition.TargetName})
	}
	issue := map[string]any{
		"workspaceId": caps.WorkspaceID, "taskId": caps.TaskID, "taskResourceVersion": caps.TaskResourceVersion,
		"provider": caps.Provider, "sourceId": caps.SourceID, "identifier": caps.Identifier,
		"url": caps.URL, "title": caps.Title, "statusId": caps.StatusID, "statusName": caps.StatusName,
		"transitions": transitions, "resourceVersion": caps.ResourceVersion, "observedAt": caps.ObservedAt,
	}
	writeback := map[string]any{
		"comment":    capabilityMethodStatus(capability, "CommentSourceIssueExact"),
		"transition": capabilityMethodStatus(capability, "TransitionSourceIssueExact"),
	}
	return actionJSON(http.StatusOK, map[string]any{"capabilities": issue, "receipt": receipt, "approvalRevision": capability.ApprovalRevision, "writeback": writeback, "hostAvailable": exact != nil})
}

func capabilityMethodStatus(capability *pluginsdk.CapabilityContext, method string) map[string]any {
	for _, operation := range capability.Operations {
		if operation.Method == method {
			reason := operation.UnavailableReason
			if operation.Supported && !operation.Authorized && reason == "" {
				reason = "Grant this capability in the plugin workspace settings"
			}
			return map[string]any{"supported": operation.Supported, "authorized": operation.Authorized, "reason": reason}
		}
	}
	return map[string]any{"supported": false, "authorized": false, "reason": "Host did not advertise this operation"}
}

func (p *coordinatorPlugin) writeLinkedIssue(ctx context.Context, workspaceID string, input issueWriteRequest, operation string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if input.RequestID == "" || input.TaskID == "" || (operation == "comment" && strings.TrimSpace(input.Body) == "") || (operation == "transition" && input.TargetID == "") || len(input.Body) > 10000 {
		return actionError(http.StatusBadRequest, "linked issue write needs a stable identity and bounded operation input"), nil
	}
	method := map[string]string{"comment": "CommentSourceIssueExact", "transition": "TransitionSourceIssueExact"}[operation]
	if method == "" {
		return actionError(http.StatusBadRequest, "linked issue operation is invalid"), nil
	}
	_, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, method)
	if result != nil {
		return result, nil
	}
	input.RequestID, input.IdempotencyKey = defaultIDs(input.RequestID, input.IdempotencyKey)
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	requestJSON, _ := json.Marshal(map[string]string{"operation": operation, "body": input.Body, "targetId": input.TargetID})
	intent, duplicate, err := store.BeginSourceWrite(ctx, sourceWriteIntent{
		WorkspaceID: workspaceID, InstanceKey: instance.Key, TaskID: input.TaskID,
		RequestID: input.RequestID, IdempotencyKey: input.IdempotencyKey, PayloadJSON: string(requestJSON),
	})
	if err != nil {
		return actionError(http.StatusConflict, "linked issue request identity conflicts with a prior operation"), nil
	}
	var snapshot struct {
		Operation                   string `json:"operation"`
		Body                        string `json:"body"`
		TargetID                    string `json:"targetId"`
		ExpectedTaskResourceVersion string `json:"expectedTaskResourceVersion"`
		ExpectedSourceVersion       string `json:"expectedSourceVersion"`
	}
	if duplicate && intent.PayloadJSON != intent.RequestJSON {
		_ = json.Unmarshal([]byte(intent.PayloadJSON), &snapshot)
	}
	if snapshot.Operation == "" {
		if _, _, result := p.exactCapability(ctx, p.Host(), workspaceID, "GetSourceIssueCapabilitiesExact"); result != nil {
			return result, nil
		}
		manager, ok := pluginsdk.HostSourceIssueWriteback(p.Host())
		if !ok {
			return actionError(http.StatusNotImplemented, "linked issue operations are unavailable"), nil
		}
		command, capabilities, _, err := manager.GetCapabilities(ctx, pluginsdk.SourceIssueCapabilitiesQuery{
			RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskID: input.TaskID,
		})
		if err != nil || !commandSucceeded(command) || capabilities == nil {
			return actionError(http.StatusBadGateway, "Host could not read the current linked issue"), nil
		}
		snapshot = struct {
			Operation                   string `json:"operation"`
			Body                        string `json:"body"`
			TargetID                    string `json:"targetId"`
			ExpectedTaskResourceVersion string `json:"expectedTaskResourceVersion"`
			ExpectedSourceVersion       string `json:"expectedSourceVersion"`
		}{operation, input.Body, input.TargetID, capabilities.TaskResourceVersion, capabilities.ResourceVersion}
		encoded, _ := json.Marshal(snapshot)
		if err := store.SaveSourceWriteSnapshot(ctx, workspaceID, instance.Key, input.RequestID, string(encoded)); err != nil {
			return actionError(http.StatusInternalServerError, "linked issue intent could not be fenced before the write"), nil
		}
	}
	manager, ok := pluginsdk.HostSourceIssueWriteback(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "linked issue operations are unavailable"), nil
	}
	commandInput := pluginsdk.SourceIssueWritebackCommand{
		RequestID: input.RequestID, WorkspaceID: workspaceID, TaskID: input.TaskID, IdempotencyKey: input.IdempotencyKey,
		ExpectedTaskResourceVersion: snapshot.ExpectedTaskResourceVersion, ExpectedSourceResourceVersion: snapshot.ExpectedSourceVersion,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		Body: snapshot.Body, TargetID: snapshot.TargetID,
	}
	var command *pluginsdk.CommandResult
	var receipt *pluginsdk.SourceIssueWritebackReceipt
	if snapshot.Operation == "comment" {
		command, receipt, err = manager.Comment(ctx, commandInput)
	} else if snapshot.Operation == "transition" {
		command, receipt, err = manager.Transition(ctx, commandInput)
	} else {
		return actionError(http.StatusConflict, "stored linked issue operation is invalid"), nil
	}
	if err != nil {
		_ = store.FinishSourceWrite(ctx, workspaceID, instance.Key, input.RequestID, "uncertain", "host_transport_error", "")
		return actionError(http.StatusBadGateway, "linked issue result is uncertain; retry with the same request id"), nil
	}
	encodedReceipt, _ := json.Marshal(receipt)
	state := string(command.Status)
	if !commandSucceeded(command) {
		_ = store.FinishSourceWrite(ctx, workspaceID, instance.Key, input.RequestID, state, command.Reason, string(encodedReceipt))
		return actionJSON(commandHTTPStatus(command.Status), map[string]any{"status": state, "receipt": receipt, "duplicate": duplicate})
	}
	if err := store.FinishSourceWrite(ctx, workspaceID, instance.Key, input.RequestID, state, "", string(encodedReceipt)); err != nil {
		return actionError(http.StatusInternalServerError, "Host write completed but its receipt could not be saved"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": state, "receipt": receipt, "duplicate": duplicate})
}

func (p *coordinatorPlugin) reportOutcomes(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	if len(instance.TaskIDs) == 0 {
		return actionJSON(http.StatusOK, map[string]any{"reports": []outcomeReport{}})
	}
	host := p.Host()
	if _, _, result := p.exactCapability(ctx, host, workspaceID, "ListTaskUsageExact"); result != nil {
		return result, nil
	}
	queries, ok := pluginsdk.HostExactQueries(host)
	if !ok {
		return actionError(http.StatusNotImplemented, "canonical usage observations are unavailable"), nil
	}
	usagePage, err := queries.ListTaskUsage(ctx, pluginsdk.ExactTaskUsageQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskIDs: append([]string(nil), instance.TaskIDs...),
		Page: pluginsdk.ExactReadPage{Limit: 200},
	})
	if err != nil || usagePage.PageInfo.HasMore {
		return actionError(http.StatusServiceUnavailable, "task usage observations are incomplete"), nil
	}
	usageByTask := map[string][]pluginsdk.TaskUsageObservation{}
	for _, usage := range usagePage.Items {
		usageByTask[usage.TaskID] = append(usageByTask[usage.TaskID], usage)
	}
	reports := make([]outcomeReport, 0, len(instance.TaskIDs))
	for _, taskID := range instance.TaskIDs {
		observation, err := p.readTaskObservation(ctx, workspaceID, taskID)
		if err != nil {
			return actionError(http.StatusServiceUnavailable, "canonical task status is unavailable"), nil
		}
		status := observation.CanonicalStatus
		if len(observation.BlockingReasons) > 0 && status != "completed" {
			status = "blocked"
		}
		verified := observation.StatusKnown && strings.EqualFold(observation.CanonicalStatus, "completed") && len(observation.BlockingReasons) == 0
		items := usageByTask[taskID]
		var cost *float64
		costState := "unknown"
		if len(items) > 0 {
			amount := 0.0
			known := true
			measured := true
			for _, item := range items {
				if item.Currency != "USD" || item.CostSubcents == nil {
					known = false
					continue
				}
				amount += float64(*item.CostSubcents) / 10000
				if !item.CostComplete || item.CostUnknownReason != nil {
					measured = false
				}
			}
			if known {
				cost = &amount
				costState = "estimated"
				if measured {
					costState = "measured"
				}
			}
		}
		report := outcomeReport{
			WorkspaceID: workspaceID, InstanceKey: instance.Key, TaskID: taskID, Status: status,
			Verified: verified, CostUSD: cost, CostState: costState,
			Provenance: "Host task snapshot " + observation.ResourceVersion,
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := store.SaveOutcomeReport(ctx, report); err != nil {
			return actionError(http.StatusInternalServerError, "outcome report could not be saved"), nil
		}
		reports = append(reports, report)
	}
	writes, err := store.ListSourceWriteStatuses(ctx, workspaceID, instance.Key)
	if err != nil {
		return actionError(http.StatusInternalServerError, "linked issue write receipts are unavailable"), nil
	}
	uncertainWrites := unresolvedSourceWrites(writes)
	return actionJSON(http.StatusOK, map[string]any{"reports": reports, "uncertainWrites": uncertainWrites})
}

func (p *coordinatorPlugin) listOutcomes(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	if _, err := p.getInstance(ctx, workspaceID, instanceKey); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	reports, err := store.ListOutcomeReports(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "outcome reports are unavailable"), nil
	}
	writes, err := store.ListSourceWriteStatuses(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "linked issue write receipts are unavailable"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"reports": reports, "uncertainWrites": unresolvedSourceWrites(writes)})
}

func (p *coordinatorPlugin) listSourceWriteStatuses(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	if _, err := p.getInstance(ctx, workspaceID, instanceKey); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	writes, err := store.ListSourceWriteStatuses(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "linked issue write receipts are unavailable"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"writes": writes})
}

func (p *coordinatorPlugin) retrySourceWrite(ctx context.Context, workspaceID, instanceKey, requestID string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if requestID == "" {
		return actionError(http.StatusBadRequest, "write request identity is required"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	intent, err := store.GetSourceWriteIntent(ctx, workspaceID, instance.Key, requestID)
	if err != nil {
		return actionError(http.StatusNotFound, "linked issue write receipt was not found"), nil
	}
	state := strings.ToLower(intent.Status)
	if state != "uncertain" && state != "dispatching" && state != "pending" {
		return actionJSON(http.StatusOK, map[string]any{"status": intent.Status, "receipt_json": intent.ReceiptJSON, "already_terminal": true})
	}
	var request struct {
		Operation string `json:"operation"`
		Body      string `json:"body"`
		TargetID  string `json:"targetId"`
	}
	if intent.RequestJSON == "" || json.Unmarshal([]byte(intent.RequestJSON), &request) != nil {
		return actionError(http.StatusConflict, "the stored linked issue request cannot be retried safely"), nil
	}
	if request.Operation != "comment" && request.Operation != "transition" {
		return actionError(http.StatusConflict, "the stored linked issue operation is invalid"), nil
	}
	return p.writeLinkedIssue(ctx, workspaceID, issueWriteRequest{
		InstanceKey: instance.Key, TaskID: intent.TaskID, RequestID: intent.RequestID,
		IdempotencyKey: intent.IdempotencyKey, Body: request.Body, TargetID: request.TargetID,
	}, request.Operation)
}

func unresolvedSourceWrites(writes []sourceWriteStatus) []sourceWriteStatus {
	items := make([]sourceWriteStatus, 0, len(writes))
	for _, write := range writes {
		switch strings.ToLower(write.Status) {
		case "pending", "dispatching", "uncertain":
			items = append(items, write)
		}
	}
	return items
}

var _ = fmt.Sprintf

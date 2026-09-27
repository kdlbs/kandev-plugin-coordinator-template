package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const (
	createTaskTool = "coordinator_create_task"
	rememberTool   = "coordinator_remember"
	recallTool     = "coordinator_recall"
)

var instanceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type coordinatorInstance struct {
	Key                  string   `json:"key"`
	Name                 string   `json:"name"`
	Role                 string   `json:"role"`
	AgentProfileID       string   `json:"agent_profile_id"`
	ExecutorID           string   `json:"executor_id,omitempty"`
	ExecutorProfileID    string   `json:"executor_profile_id,omitempty"`
	Instructions         string   `json:"instructions"`
	TaskScope            string   `json:"task_scope"`
	EstimatedBudgetUSD   float64  `json:"estimated_budget_usd"`
	MaxConcurrentRuns    int      `json:"max_concurrent_runs"`
	Paused               bool     `json:"paused"`
	Revision             uint64   `json:"revision"`
	ConversationRevision uint64   `json:"conversation_revision"`
	TaskIDs              []string `json:"task_ids"`
}

type coordinatorState struct {
	Instances []coordinatorInstance `json:"instances"`
}

type saveInstanceRequest struct {
	Key                string  `json:"instance_key"`
	Name               string  `json:"name"`
	Role               string  `json:"role"`
	AgentProfileID     string  `json:"agent_profile_id"`
	ExecutorID         string  `json:"executor_id"`
	ExecutorProfileID  string  `json:"executor_profile_id"`
	Instructions       string  `json:"instructions"`
	TaskScope          string  `json:"task_scope"`
	EstimatedBudgetUSD float64 `json:"estimated_budget_usd"`
	MaxConcurrentRuns  int     `json:"max_concurrent_runs"`
	ExpectedRevision   uint64  `json:"expected_revision"`
}

type taskRequest struct {
	InstanceKey   string `json:"instance_key"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Priority      string `json:"priority"`
	RequestID     string `json:"request_id"`
	FollowupDepth int    `json:"followup_depth,omitempty"`
}

type permissionResponseRequest struct {
	Key             string `json:"instance_key"`
	RequestID       string `json:"request_id"`
	InteractionID   string `json:"interaction_id"`
	ResourceVersion string `json:"expected_resource_version"`
	OptionID        string `json:"option_id"`
	Cancelled       bool   `json:"cancelled"`
	ReceiptID       string `json:"human_response_receipt_id"`
}

type clarificationAnswerRequest struct {
	QuestionID      string   `json:"question_id"`
	SelectedOptions []string `json:"selected_options"`
	CustomText      string   `json:"custom_text"`
}

type clarificationResponseRequest struct {
	Key             string                       `json:"instance_key"`
	RequestID       string                       `json:"request_id"`
	InteractionID   string                       `json:"interaction_id"`
	ResourceVersion string                       `json:"expected_resource_version"`
	Answers         []clarificationAnswerRequest `json:"answers"`
	ReceiptID       string                       `json:"human_response_receipt_id"`
}

type commandEnvelope struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func (p *coordinatorPlugin) HandleAction(ctx context.Context, request *pluginsdk.PluginActionRequest) (*pluginsdk.PluginActionResponse, error) {
	if request == nil || request.Context.WorkspaceID == "" {
		return actionError(http.StatusBadRequest, "workspace context is required"), nil
	}
	switch request.ActionKey {
	case "instance.catalog":
		return p.instanceCatalog(ctx, request.Context.WorkspaceID)
	case "instance.list":
		return p.listInstances(ctx, request.Context.WorkspaceID)
	case "instance.save":
		var input saveInstanceRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.saveInstance(ctx, request.Context.WorkspaceID, input)
	case "instance.pause":
		var input struct {
			Key              string `json:"instance_key"`
			ExpectedRevision uint64 `json:"expected_revision"`
			Paused           bool   `json:"paused"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.setPaused(ctx, request.Context.WorkspaceID, input.Key, input.ExpectedRevision, input.Paused)
	case "task.delegate":
		var input taskRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		if input.RequestID == "" {
			input.RequestID, _ = newRequestID()
		}
		return p.delegateTask(ctx, request.Context.WorkspaceID, input)
	case "proposal.create":
		var input struct {
			InstanceKey string `json:"instance_key"`
			ProposalID  string `json:"proposal_id"`
			SourceID    string `json:"source_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.createProposal(ctx, request.Context.WorkspaceID, input.InstanceKey, input.ProposalID, input.SourceID, taskRequest{
			InstanceKey: input.InstanceKey, Title: input.Title, Description: input.Description, Priority: input.Priority,
		})
	case "proposal.list":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listProposals(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "proposal.approve":
		var input struct {
			ProposalID string `json:"proposal_id"`
			Revision   uint64 `json:"revision"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.approveProposal(ctx, request.Context.WorkspaceID, input.ProposalID, input.Revision)
	case "proposal.reject":
		var input struct {
			ProposalID string `json:"proposal_id"`
			Revision   uint64 `json:"revision"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.rejectProposal(ctx, request.Context.WorkspaceID, input.ProposalID, input.Revision)
	case "proposal.reconcile":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.reconcileProposals(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "routine.list":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listRoutines(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "routine.create":
		var input routineCreateRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.createRoutine(ctx, request.Context.WorkspaceID, input)
	case "routine.enable":
		var input routineEnableRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.setRoutineEnabled(ctx, request.Context.WorkspaceID, input)
	case "routine.delete":
		var input routineDeleteRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.deleteRoutine(ctx, request.Context.WorkspaceID, input)
	case "watch.set":
		var input watchSetRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.setTaskWatch(ctx, request.Context.WorkspaceID, input)
	case "watch.list":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listTaskWatches(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "watch.reconcile":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.reconcileTaskWatches(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "task.adopt":
		var input taskAdoptRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.adoptTask(ctx, request.Context.WorkspaceID, input)
	case "task.release":
		var input taskAdoptRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.releaseTask(ctx, request.Context.WorkspaceID, input)
	case "completion.criteria":
		var input completionCriteriaRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.setCompletionCriteria(ctx, request.Context.WorkspaceID, input)
	case "completion.evidence":
		var input completionEvidenceRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.submitCompletionEvidence(ctx, request.Context.WorkspaceID, input)
	case "issue.capabilities":
		var input struct {
			TaskID string `json:"task_id"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.issueCapabilities(ctx, request.Context.WorkspaceID, input.TaskID)
	case "issue.comment":
		var input issueWriteRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.writeLinkedIssue(ctx, request.Context.WorkspaceID, input, "comment")
	case "issue.transition":
		var input issueWriteRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.writeLinkedIssue(ctx, request.Context.WorkspaceID, input, "transition")
	case "issue.writes.list":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listSourceWriteStatuses(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "issue.write.retry":
		var input struct {
			InstanceKey string `json:"instance_key"`
			RequestID   string `json:"request_id"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.retrySourceWrite(ctx, request.Context.WorkspaceID, input.InstanceKey, input.RequestID)
	case "workspace.admin.catalog":
		return p.workspaceAdminCatalog(ctx, request.Context.WorkspaceID)
	case "workspace.admin.apply":
		var input workspaceAdminActionRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.applyWorkspaceAdmin(ctx, request.Context.WorkspaceID, input)
	case "outcome.report":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.reportOutcomes(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "outcome.list":
		var input struct {
			InstanceKey string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listOutcomes(ctx, request.Context.WorkspaceID, input.InstanceKey)
	case "task.status":
		var input struct {
			Key string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.taskStatus(ctx, request.Context.WorkspaceID, input.Key)
	case "conversation.status":
		var input struct {
			Key string `json:"instance_key"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.conversationStatus(ctx, request.Context.WorkspaceID, input.Key)
	case "conversation.inputs":
		var input struct {
			Key            string `json:"instance_key"`
			SequenceCursor uint64 `json:"sequence_cursor"`
			Limit          uint32 `json:"limit"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.listInputs(ctx, request.Context.WorkspaceID, input.Key, input.SequenceCursor, input.Limit)
	case "conversation.enqueue":
		var input struct {
			Key              string `json:"instance_key"`
			RequestID        string `json:"request_id"`
			IdempotencyKey   string `json:"idempotency_key"`
			ExpectedRevision uint64 `json:"expected_revision"`
			OccurrenceKey    string `json:"occurrence_key"`
			Payload          string `json:"payload"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.enqueueInput(ctx, request.Context.WorkspaceID, input.Key, input.RequestID, input.IdempotencyKey, input.ExpectedRevision, input.OccurrenceKey, input.Payload)
	case "conversation.cancel":
		var input struct {
			Key                 string `json:"instance_key"`
			RequestID           string `json:"request_id"`
			IdempotencyKey      string `json:"idempotency_key"`
			ExpectedRevision    uint64 `json:"expected_revision"`
			HostInputID         string `json:"host_input_id"`
			ExpectedExecutionID string `json:"expected_execution_id"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.cancelInput(ctx, request.Context.WorkspaceID, input.Key, input.RequestID, input.IdempotencyKey, input.ExpectedRevision, input.HostInputID, input.ExpectedExecutionID)
	case "conversation.pause":
		var input struct {
			Key              string `json:"instance_key"`
			RequestID        string `json:"request_id"`
			IdempotencyKey   string `json:"idempotency_key"`
			ExpectedRevision uint64 `json:"expected_revision"`
			Paused           bool   `json:"paused"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		input.RequestID, input.IdempotencyKey = defaultIDs(input.RequestID, input.IdempotencyKey)
		return p.setPaused(ctx, request.Context.WorkspaceID, input.Key, input.ExpectedRevision, input.Paused)
	case "conversation.recover":
		var input struct {
			Key              string `json:"instance_key"`
			RequestID        string `json:"request_id"`
			IdempotencyKey   string `json:"idempotency_key"`
			ExpectedRevision uint64 `json:"expected_revision"`
			SessionVersion   string `json:"expected_session_resource_version"`
			ExecutionID      string `json:"expected_execution_id"`
		}
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.recoverConversation(ctx, request.Context.WorkspaceID, input.Key, input.RequestID, input.IdempotencyKey, input.ExpectedRevision, input.SessionVersion, input.ExecutionID)
	case "conversation.permission-response":
		var input permissionResponseRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.respondPermission(ctx, request.Context.WorkspaceID, input)
	case "conversation.clarification-response":
		var input clarificationResponseRequest
		if err := decodeActionBody(request.Body, &input); err != nil {
			return actionError(http.StatusBadRequest, err.Error()), nil
		}
		return p.answerClarification(ctx, request.Context.WorkspaceID, input)
	default:
		return actionError(http.StatusNotFound, "unknown coordinator action"), nil
	}
}

func (p *coordinatorPlugin) createProposal(ctx context.Context, workspaceID, instanceKey, proposalID, sourceID string, task taskRequest) (*pluginsdk.PluginActionResponse, error) {
	if strings.TrimSpace(instanceKey) == "" || strings.TrimSpace(task.Title) == "" || len(task.Title) > 300 || len(task.Description) > 20000 {
		return actionError(http.StatusBadRequest, "proposal needs an instance, a title up to 300 characters, and instructions up to 20000 characters"), nil
	}
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if instance.Paused {
		return actionError(http.StatusConflict, "paused instances cannot create proposals"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		return actionError(http.StatusBadRequest, "proposal input is invalid"), nil
	}
	proposal, duplicate, err := store.CreateProposal(ctx, workspaceID, instanceKey, proposalID, sourceID, string(encoded))
	if err != nil {
		return actionError(http.StatusConflict, "proposal could not be recorded"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"proposal": proposal, "duplicate": duplicate})
}

func (p *coordinatorPlugin) listProposals(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	if _, err := p.getInstance(ctx, workspaceID, instanceKey); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	proposals, err := store.ListProposals(ctx, workspaceID, instanceKey, 50)
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator proposals are unavailable"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"proposals": proposals})
}

func (p *coordinatorPlugin) approveProposal(ctx context.Context, workspaceID, proposalID string, revision uint64) (*pluginsdk.PluginActionResponse, error) {
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	proposal, outbox, _, err := store.ApproveProposal(ctx, workspaceID, proposalID, revision)
	if err != nil {
		return actionError(http.StatusConflict, "proposal changed or could not be approved"), nil
	}
	if outbox.OutboxID == "" {
		return actionJSON(http.StatusOK, map[string]any{"proposal": proposal, "status": proposal.Approval, "task_id": proposal.TaskID})
	}
	return p.dispatchProposal(ctx, workspaceID, outbox)
}

func (p *coordinatorPlugin) rejectProposal(ctx context.Context, workspaceID, proposalID string, revision uint64) (*pluginsdk.PluginActionResponse, error) {
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	proposal, err := store.RejectProposal(ctx, workspaceID, proposalID, revision)
	if err != nil {
		return actionError(http.StatusConflict, "proposal changed or could not be rejected"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"proposal": proposal})
}

func (p *coordinatorPlugin) reconcileProposals(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	if _, err := p.getInstance(ctx, workspaceID, instanceKey); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	items, err := store.ListRecoverableOutbox(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "proposal outbox is unavailable"), nil
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		response, dispatchErr := p.dispatchProposal(ctx, workspaceID, item)
		if dispatchErr != nil {
			results = append(results, map[string]any{"outbox_id": item.OutboxID, "status": "uncertain"})
			continue
		}
		var result map[string]any
		_ = json.Unmarshal(response.Body, &result)
		results = append(results, result)
	}
	return actionJSON(http.StatusOK, map[string]any{"results": results})
}

func (p *coordinatorPlugin) dispatchProposal(ctx context.Context, workspaceID string, outbox outboxRecord) (*pluginsdk.PluginActionResponse, error) {
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), err
	}
	instance, err := p.getInstance(ctx, workspaceID, outbox.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), err
	}
	if outbox.Status == "completed" {
		var receipt map[string]any
		_ = json.Unmarshal([]byte(outbox.ReceiptJSON), &receipt)
		return actionJSON(http.StatusOK, map[string]any{"status": "completed", "task_id": receipt["task_id"], "outbox_id": outbox.OutboxID, "proposal_id": outbox.ProposalID})
	}
	if instance.Paused {
		_ = store.MarkOutbox(ctx, outbox.OutboxID, "held", "instance_paused", "", "")
		return actionJSON(http.StatusConflict, map[string]any{"status": "held", "reason": "instance_paused", "outbox_id": outbox.OutboxID})
	}
	var task taskRequest
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &task); err != nil {
		_ = store.MarkOutbox(ctx, outbox.OutboxID, "failed", "invalid_payload", "", "")
		return actionError(http.StatusInternalServerError, "proposal payload is invalid"), err
	}
	task.InstanceKey = outbox.InstanceKey
	task.RequestID = outbox.IdempotencyKey
	if err := store.MarkOutbox(ctx, outbox.OutboxID, "dispatching", "", "", ""); err != nil {
		return actionError(http.StatusInternalServerError, "proposal intent could not be updated"), err
	}
	response, err := p.createDelegatedTask(ctx, workspaceID, task)
	if err != nil {
		_ = store.MarkOutbox(ctx, outbox.OutboxID, "uncertain", "host_transport_error", "", "")
		return response, err
	}
	var result map[string]any
	if json.Unmarshal(response.Body, &result) != nil || response.Status < 200 || response.Status >= 300 {
		_ = store.MarkOutbox(ctx, outbox.OutboxID, "uncertain", responseErrorText(response), "", "")
		return response, fmt.Errorf("host task creation is uncertain")
	}
	receipt, _ := json.Marshal(map[string]any{"status": "completed", "task_id": result["task_id"], "task": result["task"]})
	if err := store.MarkOutbox(ctx, outbox.OutboxID, "completed", "", string(receipt), fmt.Sprint(result["task_id"])); err != nil {
		return actionError(http.StatusInternalServerError, "task was created but its proposal receipt could not be saved"), err
	}
	return actionJSON(http.StatusOK, map[string]any{"status": "completed", "task_id": result["task_id"], "outbox_id": outbox.OutboxID, "proposal_id": outbox.ProposalID})
}

func (p *coordinatorPlugin) instanceCatalog(ctx context.Context, workspaceID string) (*pluginsdk.PluginActionResponse, error) {
	host := p.Host()
	if host == nil {
		return actionError(http.StatusNotImplemented, "Host is unavailable"), nil
	}
	profiles, _, profileErr := host.AgentProfiles().List(ctx, pluginsdk.Page{Limit: 100})
	if profileErr != nil {
		return actionError(http.StatusBadGateway, "Host could not list agent profiles"), nil
	}
	agentProfileOptions := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		agentProfileOptions = append(agentProfileOptions, map[string]any{
			"id": profile.ID, "displayName": profile.DisplayName, "name": profile.Name,
			"agentId": profile.AgentID, "model": profile.Model,
		})
	}
	workspace := pluginsdk.Workspace{}
	if queries, ok := pluginsdk.HostExactQueries(host); ok {
		if _, _, response := p.exactCapability(ctx, host, workspaceID, "ListWorkspacesExact"); response == nil {
			page, err := queries.ListWorkspaces(ctx, pluginsdk.ExactWorkspaceQuery{
				RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, Page: pluginsdk.ExactReadPage{Limit: 50},
			})
			if err == nil {
				for _, item := range page.Items {
					if item.Workspace.ID == workspaceID {
						workspace = item.Workspace
						break
					}
				}
			}
		}
	}
	executorProfiles := []pluginsdk.ExecutorProfile{}
	if reader, ok := pluginsdk.ExecutorProfiles(host); ok {
		listed, _, err := reader.List(ctx, pluginsdk.Page{Limit: 100})
		if err == nil {
			executorProfiles = listed
		}
	}
	executorProfileOptions := make([]map[string]any, 0, len(executorProfiles))
	for _, profile := range executorProfiles {
		executorProfileOptions = append(executorProfileOptions, map[string]any{
			"id": profile.ID, "displayName": profile.DisplayName, "executorType": profile.ExecutorType,
		})
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	roles, err := store.ListRoles(ctx)
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator role templates are unavailable"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{
		"agentProfiles": agentProfileOptions, "executorProfiles": executorProfileOptions,
		"roles":                 roles,
		"defaultAgentProfileId": stringValue(workspace.DefaultAgentProfileID),
		"defaultExecutorId":     stringValue(workspace.DefaultExecutorID),
	})
}

func (p *coordinatorPlugin) taskStatus(ctx context.Context, workspaceID, key string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, key)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if len(instance.TaskIDs) == 0 {
		return actionJSON(http.StatusOK, map[string]any{"tasks": []map[string]any{}})
	}
	host := p.Host()
	queries, ok := pluginsdk.HostExactQueries(host)
	if !ok {
		return actionError(http.StatusNotImplemented, "canonical task observations are unavailable"), nil
	}
	if _, _, result := p.exactCapability(ctx, host, workspaceID, "GetTaskExact"); result != nil {
		return result, nil
	}
	tasks := make([]map[string]any, 0, len(instance.TaskIDs))
	for _, taskID := range instance.TaskIDs {
		observation, _, queryErr := queries.GetTask(ctx, pluginsdk.ExactTaskGetQuery{
			RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskID: taskID,
		})
		if queryErr != nil {
			tasks = append(tasks, map[string]any{"taskId": taskID, "statusKnown": false, "canonicalStatus": "unknown"})
			continue
		}
		item := map[string]any{
			"taskId": taskID, "title": observation.Task.Title, "canonicalStatus": observation.CanonicalStatus,
			"executionState": observation.ExecutionState, "statusKnown": observation.StatusKnown,
			"resourceVersion": observation.ResourceVersion, "blockingReasons": observation.BlockingReasons,
		}
		if store, storeErr := p.policyStore(); storeErr == nil {
			if claim, claimErr := store.GetTaskClaim(ctx, workspaceID, instance.Key, taskID); claimErr == nil {
				item["lastCoordinatorClaimGeneration"] = claim.Generation
			}
		}
		tasks = append(tasks, item)
	}
	policy, policyErr := p.coordinatorPolicy(ctx, workspaceID, instance)
	if policyErr != nil {
		policy = coordinatorPolicyStatus{UsageState: "unknown", Error: "Host usage or task status is unavailable"}
	}
	return actionJSON(http.StatusOK, map[string]any{"tasks": tasks, "policy": policy})
}

type conversationSnapshot struct {
	WorkspaceID            string           `json:"workspaceId"`
	InstanceKey            string           `json:"instanceKey"`
	TaskID                 string           `json:"taskId"`
	SessionID              *string          `json:"sessionId"`
	Revision               uint64           `json:"revision"`
	SessionResourceVersion string           `json:"sessionResourceVersion"`
	ExecutionID            string           `json:"executionId,omitempty"`
	SessionState           string           `json:"sessionState,omitempty"`
	State                  string           `json:"state"`
	ReadOnly               bool             `json:"readOnly"`
	StatusReason           string           `json:"statusReason,omitempty"`
	RecoverySupported      bool             `json:"recoverySupported"`
	RecoveryReason         string           `json:"recoveryReason,omitempty"`
	PendingInteractions    []map[string]any `json:"pendingInteractions"`
}

type managedInputDTO struct {
	HostInputID          string `json:"hostInputId"`
	OccurrenceKey        string `json:"occurrenceKey"`
	Sequence             uint64 `json:"sequence"`
	Origin               string `json:"origin"`
	Payload              string `json:"payload"`
	CoalesceKey          string `json:"coalesceKey"`
	ConversationRevision uint64 `json:"conversationRevision"`
	State                string `json:"state"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	QueueEntryID         string `json:"queueEntryId"`
	ExecutionID          string `json:"executionId"`
	TurnID               string `json:"turnId"`
	SupersededBy         string `json:"supersededBy"`
}

func (p *coordinatorPlugin) conversationStatus(ctx context.Context, workspaceID, key string) (*pluginsdk.PluginActionResponse, error) {
	if !instanceKeyPattern.MatchString(key) {
		return actionError(http.StatusBadRequest, "valid instance key is required"), nil
	}
	if _, err := p.getInstance(ctx, workspaceID, key); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	host := p.Host()
	exact, capability, result := p.exactCapability(ctx, host, workspaceID, "GetManagedAgentConversationStatusExact")
	if result != nil {
		state := "unavailable"
		if result.Status == http.StatusForbidden {
			state = "revoked"
		} else if result.Status == http.StatusNotImplemented {
			state = "unsupported"
		}
		return actionJSON(http.StatusOK, conversationSnapshot{
			WorkspaceID: workspaceID, InstanceKey: key, State: state,
			StatusReason: responseErrorText(result), PendingInteractions: []map[string]any{},
		})
	}
	conversation, err := exact.ManagedAgentConversations().Get(ctx, pluginsdk.ManagedAgentConversationQuery{
		WorkspaceID: workspaceID, InstanceKey: key, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionJSON(http.StatusOK, conversationSnapshot{
			WorkspaceID: workspaceID, InstanceKey: key, State: "unavailable", StatusReason: "retained conversation is unavailable",
			PendingInteractions: []map[string]any{},
		})
	}
	snapshot := conversationSnapshot{
		WorkspaceID: workspaceID, InstanceKey: key, TaskID: conversation.TaskID, Revision: conversation.Revision,
		State: "ready", ReadOnly: conversation.Detached, PendingInteractions: []map[string]any{},
	}
	if conversation.SessionID != "" {
		snapshot.SessionID = &conversation.SessionID
	}
	if conversation.Detached {
		snapshot.State = "detached"
		snapshot.ReadOnly = true
		snapshot.StatusReason = "conversation is retained as a read-only transcript"
	} else if conversation.DesiredPaused {
		snapshot.State = "paused"
	}
	if queries, ok := pluginsdk.HostExactQueries(host); ok {
		if operationAuthorized(capability, "ListSessionsExact") {
			page, queryErr := queries.ListSessions(ctx, pluginsdk.ExactSessionQuery{
				RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID,
				Filter: pluginsdk.SessionFilter{TaskIDs: []string{conversation.TaskID}},
				Page:   pluginsdk.ExactReadPage{Limit: 50},
			})
			if queryErr == nil && len(page.Items) > 0 {
				session := page.Items[0]
				snapshot.SessionResourceVersion = session.ResourceVersion
				snapshot.ExecutionID = session.ExecutionID
				snapshot.SessionState = session.Session.State
				if operationAuthorized(capability, "RecoverSessionExact") && session.ExecutionID != "" && session.ResourceVersion != "" && session.ExecutionState == "interrupted" {
					snapshot.RecoverySupported = true
				} else if session.ExecutionState != "" && session.ExecutionState != "idle" && session.ExecutionState != "running" {
					snapshot.RecoveryReason = "Host does not report a safe recovery action for this session"
				}
			}
		}
		if operationAuthorized(capability, "ListPendingInteractionsExact") && conversation.SessionID != "" {
			page, queryErr := queries.ListPendingInteractions(ctx, pluginsdk.ExactInteractionQuery{
				RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID,
				Filter: pluginsdk.InteractionFilter{SessionIDs: []string{conversation.SessionID}},
				Page:   pluginsdk.ExactReadPage{Limit: 50},
			})
			if queryErr == nil {
				for _, item := range page.Items {
					snapshot.PendingInteractions = append(snapshot.PendingInteractions, interactionDTO(item.Interaction, item.ResourceVersion))
				}
			}
		}
	}
	return actionJSON(http.StatusOK, snapshot)
}

func (p *coordinatorPlugin) listInputs(ctx context.Context, workspaceID, key string, cursor uint64, limit uint32) (*pluginsdk.PluginActionResponse, error) {
	manager, capability, result := p.conversationManager(ctx, workspaceID, key, "ListManagedAgentInputsExact")
	if result != nil {
		return result, nil
	}
	if limit == 0 || limit > 100 {
		limit = 50
	}
	page, err := manager.ListInputs(ctx, pluginsdk.ManagedAgentInputListQuery{
		WorkspaceID: workspaceID, InstanceKey: key, SequenceCursor: cursor, Limit: limit,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not list managed conversation inputs"), nil
	}
	inputs := make([]managedInputDTO, 0, len(page.Inputs))
	for _, input := range page.Inputs {
		inputs = append(inputs, inputDTO(input))
	}
	return actionJSON(http.StatusOK, map[string]any{"inputs": inputs, "nextSequenceCursor": page.NextSequenceCursor, "hasMore": page.HasMore})
}

func (p *coordinatorPlugin) enqueueInput(ctx context.Context, workspaceID, key, requestID, idempotencyKey string, revision uint64, occurrence, payload string) (*pluginsdk.PluginActionResponse, error) {
	if len(payload) == 0 || len(payload) > 65536 || occurrence == "" {
		return actionError(http.StatusBadRequest, "message and occurrence key are required"), nil
	}
	requestID, idempotencyKey = defaultIDs(requestID, idempotencyKey)
	manager, capability, result := p.conversationManager(ctx, workspaceID, key, "EnqueueManagedAgentInputExact")
	if result != nil {
		return result, nil
	}
	command, input, err := manager.EnqueueInput(ctx, pluginsdk.ManagedAgentInputEnqueue{
		RequestID: requestID, IdempotencyKey: idempotencyKey, WorkspaceID: workspaceID, InstanceKey: key,
		ExpectedConversationRevision: revision, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		OccurrenceKey: occurrence, Origin: pluginsdk.ManagedAgentInputHuman, Payload: payload,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not queue the conversation message"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "input": inputDTO(input)})
}

func (p *coordinatorPlugin) cancelInput(ctx context.Context, workspaceID, key, requestID, idempotencyKey string, revision uint64, inputID, executionID string) (*pluginsdk.PluginActionResponse, error) {
	if inputID == "" {
		return actionError(http.StatusBadRequest, "Host input id is required"), nil
	}
	requestID, idempotencyKey = defaultIDs(requestID, idempotencyKey)
	manager, capability, result := p.conversationManager(ctx, workspaceID, key, "CancelManagedAgentInputExact")
	if result != nil {
		return result, nil
	}
	command, receipt, err := manager.CancelInput(ctx, pluginsdk.ManagedAgentInputCancel{
		RequestID: requestID, IdempotencyKey: idempotencyKey, WorkspaceID: workspaceID, InstanceKey: key,
		HostInputID: inputID, ExpectedConversationRevision: revision, ExpectedExecutionID: executionID,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not cancel the managed conversation input"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "input": inputDTO(receipt)})
}

func (p *coordinatorPlugin) conversationManager(ctx context.Context, workspaceID, key, method string) (pluginsdk.ManagedAgentConversationManager, *pluginsdk.CapabilityContext, *pluginsdk.PluginActionResponse) {
	if _, err := p.getInstance(ctx, workspaceID, key); err != nil {
		return nil, nil, actionError(http.StatusNotFound, "coordinator instance not found")
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, method)
	if result != nil {
		return nil, nil, result
	}
	return exact.ManagedAgentConversations(), capability, nil
}

func (p *coordinatorPlugin) getInstance(ctx context.Context, workspaceID, key string) (coordinatorInstance, error) {
	state, err := p.readState(ctx, workspaceID)
	if err != nil {
		return coordinatorInstance{}, err
	}
	index := findInstance(state.Instances, key)
	if index < 0 {
		return coordinatorInstance{}, errors.New("coordinator instance not found")
	}
	return state.Instances[index], nil
}

func inputDTO(input pluginsdk.ManagedAgentInputReceipt) managedInputDTO {
	return managedInputDTO{
		HostInputID: input.HostInputID, OccurrenceKey: input.OccurrenceKey, Sequence: input.Sequence,
		Origin: string(input.Origin), Payload: input.Payload, CoalesceKey: input.CoalesceKey,
		ConversationRevision: input.ConversationRevision, State: string(input.State),
		CreatedAt: input.CreatedAt, UpdatedAt: input.UpdatedAt, QueueEntryID: input.QueueEntryID,
		ExecutionID: input.ExecutionID, TurnID: input.TurnID, SupersededBy: input.SupersededBy,
	}
}

func interactionDTO(interaction pluginsdk.Interaction, resourceVersion string) map[string]any {
	options := make([]map[string]any, 0, len(interaction.Options))
	for _, option := range interaction.Options {
		options = append(options, map[string]any{
			"id": option.OptionID, "label": option.Label, "description": option.Description,
		})
	}
	questions := make([]map[string]any, 0, len(interaction.Questions))
	for _, question := range interaction.Questions {
		questionOptions := make([]map[string]any, 0, len(question.Options))
		for _, option := range question.Options {
			questionOptions = append(questionOptions, map[string]any{
				"id": option.OptionID, "label": option.Label, "description": option.Description,
			})
		}
		questions = append(questions, map[string]any{
			"id": question.ID, "title": question.Title, "prompt": question.Prompt, "options": questionOptions,
		})
	}
	return map[string]any{
		"id": interaction.ID, "kind": interaction.Kind, "title": interaction.Title, "context": interaction.Context,
		"expectedResourceVersion": resourceVersion, "agentDisconnected": interaction.AgentDisconnected,
		"options": options, "questions": questions,
	}
}

func operationAuthorized(capability *pluginsdk.CapabilityContext, method string) bool {
	for _, operation := range capability.Operations {
		if operation.Method == method {
			return operation.Supported && operation.Authorized
		}
	}
	return false
}

func responseErrorText(response *pluginsdk.PluginActionResponse) string {
	var body map[string]string
	if json.Unmarshal(response.Body, &body) == nil {
		return body["error"]
	}
	return "Host operation is unavailable"
}

func newRequestIDOrEmpty() string {
	requestID, _ := newRequestID()
	return requestID
}

func defaultIDs(requestID, idempotencyKey string) (string, string) {
	if requestID == "" {
		requestID = newRequestIDOrEmpty()
	}
	if idempotencyKey == "" {
		idempotencyKey = requestID
	}
	return requestID, idempotencyKey
}

func (p *coordinatorPlugin) listInstances(ctx context.Context, workspaceID string) (*pluginsdk.PluginActionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.readState(ctx, workspaceID)
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not read coordinator instances"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"instances": state.Instances})
}

func (p *coordinatorPlugin) saveInstance(ctx context.Context, workspaceID string, input saveInstanceRequest) (*pluginsdk.PluginActionResponse, error) {
	if err := validateInstance(input); err != nil {
		return actionError(http.StatusBadRequest, err.Error()), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.readState(ctx, workspaceID)
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not read coordinator instances"), nil
	}
	index := findInstance(state.Instances, input.Key)
	var current coordinatorInstance
	if index >= 0 {
		current = state.Instances[index]
		if input.ExpectedRevision == 0 || input.ExpectedRevision != current.Revision {
			return actionError(http.StatusConflict, "instance settings changed; reload and try again"), nil
		}
	} else if input.ExpectedRevision != 0 {
		return actionError(http.StatusConflict, "instance no longer exists; reload and try again"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	roles, err := store.ListRoles(ctx)
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator role templates are unavailable"), nil
	}
	var selectedRole *roleTemplate
	for i := range roles {
		if roles[i].Key == input.Role {
			selectedRole = &roles[i]
			break
		}
	}
	if selectedRole == nil {
		return actionError(http.StatusBadRequest, "coordinator role is not available"), nil
	}
	if input.MaxConcurrentRuns == 0 {
		input.MaxConcurrentRuns = selectedRole.MaxConcurrentRuns
	}
	if input.MaxConcurrentRuns < 1 || input.MaxConcurrentRuns > 50 {
		return actionError(http.StatusBadRequest, "maximum concurrent runs must be between 1 and 50"), nil
	}

	host := p.Host()
	exact, capability, result := p.exactCapability(ctx, host, workspaceID, "EnsureManagedAgentConversationExact")
	if result != nil {
		return result, nil
	}
	manager := exact.ManagedAgentConversations()
	if manager == nil {
		return actionError(http.StatusNotImplemented, "managed conversations are unavailable on this host"), nil
	}
	requestID, err := newRequestID()
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not create command identity"), nil
	}
	prompt := strings.TrimSpace("Role: " + selectedRole.DisplayName + "\n\nCoordinator instance key: " + input.Key + "\nUse this exact instance_key for coordinator tools.\n\nInstructions: " + input.Instructions)
	if input.TaskScope != "" {
		prompt += "\n\nTask scope: " + input.TaskScope
	}
	if input.EstimatedBudgetUSD > 0 {
		prompt += fmt.Sprintf("\n\nEstimated discretionary budget: USD %.2f.", input.EstimatedBudgetUSD)
	}
	prompt += fmt.Sprintf("\n\nMaximum concurrent coordinator runs: %d. The plugin enforces this soft admission limit.", input.MaxConcurrentRuns)
	command, conversation, err := manager.Ensure(ctx, pluginsdk.ManagedAgentConversationSpec{
		RequestID: requestID, IdempotencyKey: requestID, WorkspaceID: workspaceID, InstanceKey: input.Key,
		ExpectedRevision: current.ConversationRevision, ApprovalRevision: capability.ApprovalRevision,
		ManifestDigest: capability.ManifestDigest, AgentProfileID: input.AgentProfileID,
		ExecutorID: input.ExecutorID, ExecutorProfileID: input.ExecutorProfileID,
		BasePrompt: prompt, InstructionVersion: fmt.Sprintf("instance-%d", current.Revision+1),
		AgentToolNames: append([]string(nil), selectedRole.DefaultAgentToolNames...),
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not ensure the retained conversation"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	if conversation.InstanceKey == "" || conversation.Revision == 0 {
		return actionError(http.StatusConflict, "Host rejected the conversation revision"), nil
	}

	updated := coordinatorInstance{
		Key: input.Key, Name: strings.TrimSpace(input.Name), Role: strings.TrimSpace(input.Role),
		AgentProfileID: input.AgentProfileID, ExecutorID: input.ExecutorID, ExecutorProfileID: input.ExecutorProfileID,
		Instructions: input.Instructions, TaskScope: input.TaskScope, EstimatedBudgetUSD: input.EstimatedBudgetUSD, MaxConcurrentRuns: input.MaxConcurrentRuns,
		Revision: current.Revision + 1, ConversationRevision: conversation.Revision,
		TaskIDs: append([]string(nil), current.TaskIDs...),
	}
	if index < 0 {
		state.Instances = append(state.Instances, updated)
	} else {
		state.Instances[index] = updated
	}
	if err := p.writeState(ctx, workspaceID, state); err != nil {
		return actionError(http.StatusInternalServerError, "conversation is ready but coordinator settings were not saved"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"instance": updated})
}

func (p *coordinatorPlugin) setPaused(ctx context.Context, workspaceID, key string, expectedRevision uint64, paused bool) (*pluginsdk.PluginActionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.readState(ctx, workspaceID)
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not read coordinator instances"), nil
	}
	index := findInstance(state.Instances, key)
	if index < 0 {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	current := state.Instances[index]
	if current.ConversationRevision != expectedRevision {
		return actionError(http.StatusConflict, "instance settings changed; reload and try again"), nil
	}
	host := p.Host()
	exact, capability, result := p.exactCapability(ctx, host, workspaceID, "SetManagedAgentConversationPausedExact")
	if result != nil {
		return result, nil
	}
	requestID, err := newRequestID()
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not create command identity"), nil
	}
	command, conversation, err := exact.ManagedAgentConversations().SetPaused(ctx, pluginsdk.ManagedAgentConversationPause{
		RequestID: requestID, IdempotencyKey: requestID, WorkspaceID: workspaceID, InstanceKey: key,
		ExpectedRevision: current.ConversationRevision, ApprovalRevision: capability.ApprovalRevision,
		ManifestDigest: capability.ManifestDigest, Paused: paused,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not update conversation pause state"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	current.Paused = paused
	current.Revision++
	current.ConversationRevision = conversation.Revision
	state.Instances[index] = current
	if err := p.writeState(ctx, workspaceID, state); err != nil {
		return actionError(http.StatusInternalServerError, "Host pause changed but coordinator state did not save"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"instance": current})
}

func (p *coordinatorPlugin) recoverConversation(ctx context.Context, workspaceID, key, requestID, idempotencyKey string, revision uint64, sessionVersion, executionID string) (*pluginsdk.PluginActionResponse, error) {
	if sessionVersion == "" || executionID == "" {
		return actionError(http.StatusBadRequest, "observed session and execution revisions are required"), nil
	}
	requestID, idempotencyKey = defaultIDs(requestID, idempotencyKey)
	managed, capability, result := p.conversationManager(ctx, workspaceID, key, "RecoverSessionExact")
	if result != nil {
		return result, nil
	}
	conversation, err := managed.Get(ctx, pluginsdk.ManagedAgentConversationQuery{
		WorkspaceID: workspaceID, InstanceKey: key, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not read the managed conversation"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	claim, err := store.GetTaskClaim(ctx, workspaceID, key, conversation.TaskID)
	if err != nil {
		return actionError(http.StatusForbidden, "explicit task adoption is required before execution recovery"), nil
	}
	manager, ok := pluginsdk.HostExecutionCommands(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "exact execution recovery is unavailable"), nil
	}
	command, recovered, err := manager.RecoverSession(ctx, pluginsdk.ExactSessionRecoveryCommand{
		ExactTaskExecutionCommand: pluginsdk.ExactTaskExecutionCommand{
			RequestID: requestID, WorkspaceID: workspaceID, TaskID: conversation.TaskID, SessionID: conversation.SessionID,
			ExpectedSessionResourceVersion: sessionVersion, ExpectedExecutionID: executionID,
			IdempotencyKey: idempotencyKey, ManagementInstanceKey: key, ExpectedClaimGeneration: claim.Generation,
			ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		},
		Action: "resume",
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not recover the managed conversation session"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	_ = revision
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "session_id": recovered.SessionID, "execution_id": recovered.ExecutionID})
}

func (p *coordinatorPlugin) respondPermission(ctx context.Context, workspaceID string, input permissionResponseRequest) (*pluginsdk.PluginActionResponse, error) {
	_, capability, result := p.conversationManager(ctx, workspaceID, input.Key, "RespondPermissionExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostExactInteractionCommands(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "exact human interaction responses are unavailable"), nil
	}
	command, _, err := manager.RespondPermission(ctx, pluginsdk.ExactPermissionResponse{
		RequestID: input.RequestID, WorkspaceID: workspaceID, InteractionID: input.InteractionID,
		ExpectedResourceVersion: input.ResourceVersion, OptionID: input.OptionID, Cancelled: input.Cancelled,
		HumanResponseReceiptID: input.ReceiptID, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not submit the permission response"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status)})
}

func (p *coordinatorPlugin) answerClarification(ctx context.Context, workspaceID string, input clarificationResponseRequest) (*pluginsdk.PluginActionResponse, error) {
	_, capability, result := p.conversationManager(ctx, workspaceID, input.Key, "AnswerClarificationExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostExactInteractionCommands(p.Host())
	if !ok {
		return actionError(http.StatusNotImplemented, "exact human interaction responses are unavailable"), nil
	}
	answers := make([]pluginsdk.ClarificationAnswer, 0, len(input.Answers))
	for _, answer := range input.Answers {
		answers = append(answers, pluginsdk.ClarificationAnswer{
			QuestionID: answer.QuestionID, SelectedOptions: answer.SelectedOptions, CustomText: answer.CustomText,
		})
	}
	command, _, err := manager.AnswerClarification(ctx, pluginsdk.ExactClarificationResponse{
		RequestID: input.RequestID, WorkspaceID: workspaceID, InteractionID: input.InteractionID,
		ExpectedResourceVersion: input.ResourceVersion, Answers: answers, HumanResponseReceiptID: input.ReceiptID,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not submit the clarification response"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status)})
}

func (p *coordinatorPlugin) delegateTask(ctx context.Context, workspaceID string, input taskRequest) (*pluginsdk.PluginActionResponse, error) {
	if input.RequestID == "" || input.InstanceKey == "" || strings.TrimSpace(input.Title) == "" {
		return actionError(http.StatusBadRequest, "instance, request identity, and task title are required"), nil
	}
	return p.createDelegatedTask(ctx, workspaceID, input)
}

func (p *coordinatorPlugin) createDelegatedTask(ctx context.Context, workspaceID string, input taskRequest) (*pluginsdk.PluginActionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.readState(ctx, workspaceID)
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not read coordinator instances"), nil
	}
	index := findInstance(state.Instances, input.InstanceKey)
	if index < 0 {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if state.Instances[index].Paused {
		return actionError(http.StatusConflict, "paused instances cannot delegate new tasks"), nil
	}
	policy, err := p.coordinatorPolicy(ctx, workspaceID, state.Instances[index])
	if err != nil {
		return actionError(http.StatusServiceUnavailable, "canonical task status or usage is unavailable; no work was created"), nil
	}
	if policy.Blocked {
		return actionJSON(http.StatusConflict, map[string]any{"status": "held", "reason": policy.Reason, "policy": policy})
	}
	host := p.Host()
	_, capability, result := p.exactCapability(ctx, host, workspaceID, "CreateTaskExact")
	if result != nil {
		return result, nil
	}
	commands, ok := pluginsdk.HostTaskCommands(host)
	if !ok {
		return actionError(http.StatusNotImplemented, "exact task commands are unavailable on this host"), nil
	}
	externalID := "coordinator:" + input.InstanceKey + ":" + input.RequestID
	command, task, err := commands.CreateTask(ctx, pluginsdk.ExactTaskCreate{
		RequestID: input.RequestID, WorkspaceID: workspaceID, IdempotencyKey: input.RequestID,
		ExternalID: externalID, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		Task: pluginsdk.CreateTaskInput{
			WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Description: input.Description,
			Priority: input.Priority,
			Metadata: map[string]any{"coordinator_instance_key": input.InstanceKey, "coordinator_role": state.Instances[index].Role},
		},
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not create the delegated task"), nil
	}
	if !commandSucceeded(command) || task == nil {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	state.Instances[index].TaskIDs = appendUnique(state.Instances[index].TaskIDs, task.ID)
	if err := p.writeState(ctx, workspaceID, state); err != nil {
		return actionError(http.StatusInternalServerError, "task was created but its coordinator link could not be saved"), nil
	}
	if err := p.installTaskWatch(ctx, workspaceID, input.InstanceKey, task.ID, input.FollowupDepth); err != nil {
		return actionError(http.StatusServiceUnavailable, "task was created but its coordinator watch could not be saved"), nil
	}
	store, err := p.policyStore()
	if err != nil || store.SaveMemory(ctx, workspaceID, input.InstanceKey, "last_delegated_task", task.ID) != nil {
		return actionError(http.StatusInternalServerError, "task was created but coordinator memory could not be saved"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "task_id": task.ID, "task": task})
}

func (p *coordinatorPlugin) InvokeAgentTool(ctx context.Context, request *pluginsdk.AgentToolRequest) (*pluginsdk.AgentToolResult, error) {
	if request == nil || request.Context.Surface != "managed-conversation" || request.Context.WorkspaceID == "" {
		return toolError("agent tool requires a managed conversation workspace"), nil
	}
	if !contains(request.Context.AgentToolNames, request.Name) {
		return toolError("tool is not enabled for this managed conversation"), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	host := p.Host()
	if host == nil {
		return toolError("Host is unavailable"), nil
	}
	exact, ok := pluginsdk.HostV2(host)
	if !ok {
		return toolError("exact coordination operations are unavailable"), nil
	}
	capability, err := exact.GetCapabilityContext(ctx, request.Context.WorkspaceID)
	if err != nil {
		return toolError("Host capability context is unavailable"), nil
	}
	if capability.InstallationID != request.Context.InstallationID || capability.ApprovalRevision != request.Context.ApprovalRevision || capability.ManifestDigest != request.Context.ManifestDigest {
		return toolError("managed conversation authority changed; reload before using this tool"), nil
	}
	if !operationAuthorized(capability, "GetManagedAgentConversationStatusExact") {
		return toolError("managed conversation read access is unavailable"), nil
	}
	var input struct {
		InstanceKey string `json:"instance_key"`
		Key         string `json:"key"`
		Value       string `json:"value"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
	}
	encoded, err := json.Marshal(request.Arguments)
	if err != nil {
		return toolError("tool input is invalid"), nil
	}
	if err := json.Unmarshal(encoded, &input); err != nil || !instanceKeyPattern.MatchString(input.InstanceKey) {
		return toolError("tool input needs a valid instance_key"), nil
	}
	conversation, err := exact.ManagedAgentConversations().Get(ctx, pluginsdk.ManagedAgentConversationQuery{
		WorkspaceID: request.Context.WorkspaceID, InstanceKey: input.InstanceKey,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil || conversation.TaskID != request.Context.TaskID || conversation.SessionID != request.Context.SessionID || conversation.Revision != request.Context.ConversationRevision {
		return toolError("tool context does not match this coordinator instance's retained conversation"), nil
	}
	state, err := p.readState(ctx, request.Context.WorkspaceID)
	if err != nil {
		return toolError("coordinator state is unavailable"), nil
	}
	index := findInstance(state.Instances, input.InstanceKey)
	if index < 0 {
		return toolError("coordinator instance does not exist in this workspace"), nil
	}
	if state.Instances[index].Paused {
		return toolError("paused instances cannot perform policy actions"), nil
	}
	switch request.Name {
	case rememberTool:
		if strings.TrimSpace(input.Key) == "" || len(input.Key) > 128 || len(input.Value) > 4096 {
			return toolError("memory key or value is outside its limit"), nil
		}
		store, err := p.policyStore()
		if err != nil || store.SaveMemory(ctx, request.Context.WorkspaceID, input.InstanceKey, input.Key, input.Value) != nil {
			return toolError("coordinator memory could not be saved"), nil
		}
		return &pluginsdk.AgentToolResult{Text: "Memory saved for " + state.Instances[index].Name, StructuredContent: map[string]any{"saved": true}}, nil
	case recallTool:
		store, err := p.policyStore()
		if err != nil {
			return toolError("coordinator memory is unavailable"), nil
		}
		value, found, err := store.ReadMemory(ctx, request.Context.WorkspaceID, input.InstanceKey, input.Key)
		if err != nil {
			return toolError("coordinator memory is unavailable"), nil
		}
		return &pluginsdk.AgentToolResult{Text: value, StructuredContent: map[string]any{"found": found, "value": value}}, nil
	case createTaskTool:
		if request.InvocationID == "" || strings.TrimSpace(input.Title) == "" {
			return toolError("task title and invocation identity are required"), nil
		}
		// Release the plugin lock before the shared task command. The Host owns
		// command idempotency, so an exact retry remains safe across a restart.
		p.mu.Unlock()
		response, err := p.createDelegatedTask(ctx, request.Context.WorkspaceID, taskRequest{
			InstanceKey: input.InstanceKey, Title: input.Title, Description: input.Description,
			Priority: input.Priority, RequestID: "agent-" + request.InvocationID,
		})
		p.mu.Lock()
		if err != nil {
			return toolError("Host rejected delegated task creation"), nil
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body, &result); err != nil || response.Status < 200 || response.Status >= 300 {
			return toolError(fmt.Sprint(result["error"])), nil
		}
		return &pluginsdk.AgentToolResult{Text: "Delegated task created: " + fmt.Sprint(result["task_id"]), StructuredContent: result}, nil
	default:
		return toolError("unknown coordinator agent tool"), nil
	}
}

func (p *coordinatorPlugin) readMemory(ctx context.Context, workspaceID, instanceKey, key string) (string, error) {
	store, err := p.policyStore()
	if err != nil {
		return "", err
	}
	value, _, err := store.ReadMemory(ctx, workspaceID, instanceKey, key)
	return value, err
}

func (p *coordinatorPlugin) exactCapability(ctx context.Context, host pluginsdk.Host, workspaceID, method string) (pluginsdk.ExactHost, *pluginsdk.CapabilityContext, *pluginsdk.PluginActionResponse) {
	if host == nil {
		return nil, nil, actionError(http.StatusNotImplemented, "Host is unavailable")
	}
	exact, ok := pluginsdk.HostV2(host)
	if !ok {
		return nil, nil, actionError(http.StatusNotImplemented, "this Host does not support exact coordination operations")
	}
	capability, err := exact.GetCapabilityContext(ctx, workspaceID)
	if err != nil || capability == nil {
		return nil, nil, actionError(http.StatusBadGateway, "Host capability context is unavailable")
	}
	for _, operation := range capability.Operations {
		if operation.Method != method {
			continue
		}
		if !operation.Supported {
			return nil, nil, actionError(http.StatusNotImplemented, firstNonEmpty(operation.UnavailableReason, "Host does not support this operation"))
		}
		if !operation.Authorized {
			return nil, nil, actionError(http.StatusForbidden, "grant this capability in the plugin's workspace settings")
		}
		return exact, capability, nil
	}
	return nil, nil, actionError(http.StatusNotImplemented, "Host did not advertise this exact operation")
}

func (p *coordinatorPlugin) readState(ctx context.Context, workspaceID string) (coordinatorState, error) {
	store, err := p.policyStore()
	if err != nil {
		return coordinatorState{}, err
	}
	instances, err := store.ListInstances(ctx, workspaceID)
	if err != nil {
		return coordinatorState{}, err
	}
	return coordinatorState{Instances: instances}, nil
}

func (p *coordinatorPlugin) writeState(ctx context.Context, workspaceID string, state coordinatorState) error {
	store, err := p.policyStore()
	if err != nil {
		return err
	}
	return store.ReplaceInstances(ctx, workspaceID, state.Instances)
}

func validateInstance(input saveInstanceRequest) error {
	if !instanceKeyPattern.MatchString(input.Key) || input.Name != strings.TrimSpace(input.Name) || input.Role != strings.TrimSpace(input.Role) {
		return errors.New("instance key, name, and role are required")
	}
	if input.Name == "" || input.Role == "" || len(input.Name) > 80 || len(input.Role) > 80 || len(input.AgentProfileID) > 256 ||
		len(input.ExecutorID) > 256 || len(input.ExecutorProfileID) > 256 || len(input.Instructions) > 10000 || len(input.TaskScope) > 1000 ||
		input.EstimatedBudgetUSD < 0 || input.EstimatedBudgetUSD > 100000 || input.MaxConcurrentRuns < 0 || input.MaxConcurrentRuns > 50 {
		return errors.New("instance settings are outside their limits")
	}
	return nil
}

func findInstance(instances []coordinatorInstance, key string) int {
	for i := range instances {
		if instances[i].Key == key {
			return i
		}
	}
	return -1
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func decodeActionBody(body []byte, target any) error {
	if len(body) == 0 {
		return errors.New("action body is required")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("action body must be valid JSON")
	}
	return nil
}

func actionJSON(status int, value any) (*pluginsdk.PluginActionResponse, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return actionError(http.StatusInternalServerError, "could not encode coordinator response"), nil
	}
	return &pluginsdk.PluginActionResponse{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}, nil
}

func actionError(status int, message string) *pluginsdk.PluginActionResponse {
	body, _ := json.Marshal(map[string]string{"error": message})
	return &pluginsdk.PluginActionResponse{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}
}

func commandSucceeded(command *pluginsdk.CommandResult) bool {
	return command != nil && (command.Status == pluginsdk.CommandApplied || command.Status == pluginsdk.CommandAlreadyApplied || command.Status == pluginsdk.CommandNoChange)
}

func commandHTTPStatus(status pluginsdk.CommandStatus) int {
	switch status {
	case pluginsdk.CommandConflict:
		return http.StatusConflict
	case pluginsdk.CommandDenied:
		return http.StatusForbidden
	case pluginsdk.CommandNotFound:
		return http.StatusNotFound
	case pluginsdk.CommandInvalid:
		return http.StatusBadRequest
	case pluginsdk.CommandUnsupported:
		return http.StatusNotImplemented
	default:
		return http.StatusBadGateway
	}
}

func toolError(message string) *pluginsdk.AgentToolResult {
	return &pluginsdk.AgentToolResult{Text: message, IsError: true}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func newRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

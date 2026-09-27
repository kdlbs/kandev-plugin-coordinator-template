package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

type operationalTestHost struct {
	*coordinatorTestHost
	claims          *operationalClaims
	completion      *operationalCompletion
	issues          *operationalIssues
	admin           *operationalWorkspaceAdmin
	execution       *operationalExecution
	writeAuthorized bool
	adminAuthorized bool
}

func newOperationalTestHost() *operationalTestHost {
	return &operationalTestHost{
		coordinatorTestHost: newCoordinatorTestHost(),
		claims:              &operationalClaims{},
		completion:          &operationalCompletion{},
		issues:              &operationalIssues{failCommentOnce: true},
		admin:               &operationalWorkspaceAdmin{},
		execution:           &operationalExecution{},
	}
}

func (h *operationalTestHost) TaskManagementClaims() pluginsdk.ExactTaskManagementClaimCommandManager {
	return h.claims
}

func (h *operationalTestHost) TaskCompletionGates() pluginsdk.ExactTaskCompletionGateCommandManager {
	return h.completion
}

func (h *operationalTestHost) SourceIssueWriteback() pluginsdk.ExactSourceIssueWritebackManager {
	return h.issues
}

func (h *operationalTestHost) WorkspaceAdministration() pluginsdk.ExactWorkspaceAdministrationManager {
	return h.admin
}

func (h *operationalTestHost) GetCapabilityContext(ctx context.Context, workspaceID string) (*pluginsdk.CapabilityContext, error) {
	capability, err := h.coordinatorTestHost.GetCapabilityContext(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	capability.Operations = append(capability.Operations,
		pluginsdk.CapabilityOperation{Method: "AcquireTaskManagementClaimExact", CapabilityID: "host.v2.write:tasks", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "ReleaseTaskManagementClaimExact", CapabilityID: "host.v2.write:tasks", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "SetTaskCompletionCriteriaExact", CapabilityID: "host.v2.write:tasks", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "VerifyTaskCompletionCriterionExact", CapabilityID: "host.v2.write:tasks", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "RecoverSessionExact", CapabilityID: "host.v2.write:execution", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "GetSourceIssueCapabilitiesExact", CapabilityID: "host.v2.read:source_issues", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "CommentSourceIssueExact", CapabilityID: "host.v2.write:source_issues", Supported: true, Authorized: h.writeAuthorized},
		pluginsdk.CapabilityOperation{Method: "TransitionSourceIssueExact", CapabilityID: "host.v2.write:source_issues", Supported: true, Authorized: h.writeAuthorized},
		pluginsdk.CapabilityOperation{Method: "ListWorkspacesExact", CapabilityID: "host.v2.read:workspaces", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "ListWorkflowsExact", CapabilityID: "host.v2.read:workspaces", Supported: true, Authorized: true},
		pluginsdk.CapabilityOperation{Method: "CreateWorkflowExact", CapabilityID: "host.v2.write:workflows", Supported: true, Authorized: h.adminAuthorized},
		pluginsdk.CapabilityOperation{Method: "UpdateWorkflowExact", CapabilityID: "host.v2.write:workflows", Supported: true, Authorized: h.adminAuthorized},
	)
	return capability, nil
}

func (h *operationalTestHost) ExecutionCommands() pluginsdk.ExactExecutionCommandManager {
	return h.execution
}

type operationalExecution struct {
	recovery pluginsdk.ExactSessionRecoveryCommand
}

func (*operationalExecution) EnsureTaskRun(context.Context, pluginsdk.ExactTaskRunCommand) (*pluginsdk.CommandResult, pluginsdk.ExactTaskRunResult, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandUnsupported}, pluginsdk.ExactTaskRunResult{}, nil
}

func (*operationalExecution) StopTaskRun(context.Context, pluginsdk.ExactTaskExecutionCommand) (*pluginsdk.CommandResult, bool, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandUnsupported}, false, nil
}

func (m *operationalExecution) RecoverSession(_ context.Context, input pluginsdk.ExactSessionRecoveryCommand) (*pluginsdk.CommandResult, pluginsdk.ExactTaskRunResult, error) {
	m.recovery = input
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, pluginsdk.ExactTaskRunResult{SessionID: input.SessionID, ExecutionID: "recovered-execution"}, nil
}

func (*operationalExecution) CancelPendingTaskTransition(context.Context, pluginsdk.ExactPendingTransitionCommand) (*pluginsdk.CommandResult, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandUnsupported}, nil
}

func (*operationalExecution) GetSessionModeContext(context.Context, pluginsdk.ExactTaskExecutionCommand) (pluginsdk.SessionModeContext, error) {
	return pluginsdk.SessionModeContext{}, nil
}

func (*operationalExecution) SetSessionMode(context.Context, pluginsdk.ExactSessionModeCommand) (*pluginsdk.CommandResult, string, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandUnsupported}, "", nil
}

type operationalWorkspaceAdmin struct {
	command pluginsdk.WorkspaceAdminCommand
	calls   int
}

func (m *operationalWorkspaceAdmin) Apply(_ context.Context, command pluginsdk.WorkspaceAdminCommand) (*pluginsdk.CommandResult, error) {
	m.command = command
	m.calls++
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, nil
}

type operationalClaims struct {
	lastAcquire pluginsdk.ExactTaskManagementClaimAcquire
	lastRelease pluginsdk.ExactTaskManagementClaimRelease
}

func (m *operationalClaims) Acquire(_ context.Context, input pluginsdk.ExactTaskManagementClaimAcquire) (*pluginsdk.CommandResult, *pluginsdk.TaskManagementClaim, error) {
	m.lastAcquire = input
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.TaskManagementClaim{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, InstanceKey: input.InstanceKey,
		Generation: 7, ResourceVersion: "claim-v7",
	}, nil
}

func (m *operationalClaims) Release(_ context.Context, input pluginsdk.ExactTaskManagementClaimRelease) (*pluginsdk.CommandResult, *pluginsdk.TaskManagementClaim, error) {
	m.lastRelease = input
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.TaskManagementClaim{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Generation: 8, ResourceVersion: "claim-v8",
	}, nil
}

func (*operationalClaims) Transfer(context.Context, pluginsdk.ExactTaskManagementClaimTransfer) (*pluginsdk.CommandResult, *pluginsdk.TaskManagementClaim, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandUnsupported}, nil, nil
}

type operationalCompletion struct {
	criteria pluginsdk.ExactTaskCompletionCriteria
	evidence pluginsdk.ExactTaskCompletionEvidence
	revision int64
}

func (m *operationalCompletion) SetCriteria(_ context.Context, input pluginsdk.ExactTaskCompletionCriteria) (*pluginsdk.CommandResult, *pluginsdk.TaskCompletionGate, error) {
	m.criteria = input
	m.revision++
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.TaskCompletionGate{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Revision: m.revision,
		Criteria: []pluginsdk.TaskCompletionCriterion{{ID: input.Criteria[0].ID, CriterionRevision: m.revision}},
	}, nil
}

func (m *operationalCompletion) Verify(_ context.Context, input pluginsdk.ExactTaskCompletionEvidence) (*pluginsdk.CommandResult, *pluginsdk.TaskCompletionGate, error) {
	m.evidence = input
	if input.ExpectedTaskResourceVersion != "task-v1" {
		return &pluginsdk.CommandResult{Status: pluginsdk.CommandConflict, Reason: "stale_task_revision"}, &pluginsdk.TaskCompletionGate{
			WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Revision: m.revision,
		}, nil
	}
	m.revision++
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.TaskCompletionGate{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Revision: m.revision,
	}, nil
}

type operationalIssues struct {
	failCommentOnce bool
	commentCalls    []pluginsdk.SourceIssueWritebackCommand
	transitionCalls []pluginsdk.SourceIssueWritebackCommand
}

func (*operationalIssues) GetCapabilities(_ context.Context, query pluginsdk.SourceIssueCapabilitiesQuery) (*pluginsdk.CommandResult, *pluginsdk.SourceIssueCapabilities, *pluginsdk.HostReadReceipt, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.SourceIssueCapabilities{
		WorkspaceID: query.WorkspaceID, TaskID: query.TaskID, TaskResourceVersion: "task-v1",
		Provider: "jira", SourceID: "jira:ACME-12", Identifier: "ACME-12", ResourceVersion: "source-v3",
		Transitions: []pluginsdk.SourceIssueTransition{{TargetID: "in-progress", TargetName: "In progress"}},
	}, &pluginsdk.HostReadReceipt{WorkspaceID: query.WorkspaceID, SnapshotVersion: "source-snapshot-3"}, nil
}

func (m *operationalIssues) Comment(_ context.Context, input pluginsdk.SourceIssueWritebackCommand) (*pluginsdk.CommandResult, *pluginsdk.SourceIssueWritebackReceipt, error) {
	m.commentCalls = append(m.commentCalls, input)
	if m.failCommentOnce {
		m.failCommentOnce = false
		return nil, nil, errors.New("transport closed after request dispatch")
	}
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.SourceIssueWritebackReceipt{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Operation: "comment", State: "applied",
	}, nil
}

func (m *operationalIssues) Transition(_ context.Context, input pluginsdk.SourceIssueWritebackCommand) (*pluginsdk.CommandResult, *pluginsdk.SourceIssueWritebackReceipt, error) {
	m.transitionCalls = append(m.transitionCalls, input)
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &pluginsdk.SourceIssueWritebackReceipt{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID, Operation: "transition", State: "applied",
	}, nil
}

func operationalPlugin(t *testing.T) (*coordinatorPlugin, *operationalTestHost) {
	t.Helper()
	host := newOperationalTestHost()
	plugin := newCoordinatorTestPlugin(t, host.coordinatorTestHost, t.TempDir())
	plugin.SetHost(host)
	response, err := plugin.HandleAction(context.Background(), actionRequest("instance.save", "workspace-1", map[string]any{
		"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast",
	}))
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)
	return plugin, host
}

func TestCoordinatorRecoveryCarriesHostClaimGeneration(t *testing.T) {
	plugin, host := operationalPlugin(t)
	host.coordinatorTestHost.conversations.byKey["workspace-1/delivery-lead"] = pluginsdk.ManagedAgentConversationDescriptor{
		InstallationID: "installation-1", WorkspaceID: "workspace-1", InstanceKey: "delivery-lead",
		TaskID: "managed-delivery-lead", SessionID: "session-delivery-lead", Revision: 3,
	}
	store, err := plugin.policyStore()
	require.NoError(t, err)
	require.NoError(t, store.SaveTaskClaim(context.Background(), storedTaskClaim{
		WorkspaceID: "workspace-1", InstanceKey: "delivery-lead", TaskID: "managed-delivery-lead",
		Generation: 9, ResourceVersion: "claim-v9",
	}))

	response, err := plugin.recoverConversation(context.Background(), "workspace-1", "delivery-lead", "request-recover", "recover-1", 3, "session-v2", "execution-v2")
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)
	require.Equal(t, "delivery-lead", host.execution.recovery.ManagementInstanceKey)
	require.Equal(t, int64(9), host.execution.recovery.ExpectedClaimGeneration)
}

func readAction[T any](t *testing.T, response *pluginsdk.PluginActionResponse) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal(response.Body, &value))
	return value
}

func TestCoordinatorOperationalTools(t *testing.T) {
	t.Run("workspace administration is independently granted and version fenced", func(t *testing.T) {
		plugin, host := operationalPlugin(t)
		host.queries.workspaces = []pluginsdk.ExactWorkspaceObservation{{
			Workspace: pluginsdk.Workspace{ID: "workspace-1", Name: "E2E workspace"}, ResourceVersion: "workspace-v4",
		}}
		host.queries.workflows = []pluginsdk.ExactWorkflowObservation{{
			Workflow: pluginsdk.Workflow{ID: "workflow-1", Name: "Current workflow"}, ResourceVersion: "workflow-v9",
		}}
		ctx := context.Background()
		response, err := plugin.HandleAction(ctx, actionRequest("workspace.admin.catalog", "workspace-1", map[string]any{}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		catalog := readAction[struct {
			Workspace struct {
				ResourceVersion string `json:"resourceVersion"`
			} `json:"workspace"`
			Workflows []struct {
				ID              string `json:"id"`
				ResourceVersion string `json:"resourceVersion"`
			} `json:"workflows"`
			Administration map[string]map[string]any `json:"administration"`
		}](t, response)
		require.Equal(t, "workspace-v4", catalog.Workspace.ResourceVersion)
		require.Equal(t, "workflow-v9", catalog.Workflows[0].ResourceVersion)
		require.False(t, catalog.Administration["create"]["authorized"].(bool))

		request := map[string]any{
			"operation": "workflow.create", "request_id": "workflow-create-1",
			"expected_workspace_resource_version": "workspace-v4", "name": "Release readiness",
			"description": "Tracks release checks", "prompt": "Require current evidence.",
		}
		response, err = plugin.HandleAction(ctx, actionRequest("workspace.admin.apply", "workspace-1", request))
		require.NoError(t, err)
		require.Equal(t, 403, response.Status)
		require.Equal(t, 0, host.admin.calls)

		host.adminAuthorized = true
		response, err = plugin.HandleAction(ctx, actionRequest("workspace.admin.apply", "workspace-1", request))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Equal(t, "workspace-v4", host.admin.command.CreateWorkflow.ExpectedWorkspaceResourceVersion)
		require.Equal(t, "Release readiness", host.admin.command.CreateWorkflow.Name)
		require.Equal(t, "workflow-create-1", host.admin.command.IdempotencyKey)
		require.Equal(t, uint64(1), host.admin.command.ApprovalRevision)

		update := map[string]any{
			"operation": "workflow.update", "request_id": "workflow-update-1", "workflow_id": "workflow-1",
			"expected_resource_version": "workflow-v9", "description": "Updated description",
		}
		response, err = plugin.HandleAction(ctx, actionRequest("workspace.admin.apply", "workspace-1", update))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Equal(t, "workflow-v9", host.admin.command.UpdateWorkflow.ExpectedResourceVersion)
		require.Equal(t, "Updated description", *host.admin.command.UpdateWorkflow.Description)
	})

	t.Run("adoption fences versioned completion evidence", func(t *testing.T) {
		plugin, host := operationalPlugin(t)
		ctx := context.Background()

		response, err := plugin.HandleAction(ctx, actionRequest("completion.criteria", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "task_id": "task-1", "request_id": "criteria-before-adopt",
			"criteria": []map[string]any{{"id": "tests-pass", "description": "Regression tests pass", "evidence_kind": "ci_run", "evidence_id": "run-9", "evidence_revision": "head-abc"}},
		}))
		require.NoError(t, err)
		require.Equal(t, 403, response.Status)

		response, err = plugin.HandleAction(ctx, actionRequest("task.adopt", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "task_id": "task-1", "request_id": "claim-1", "reason": "Own the release validation",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Equal(t, "task-v1", host.claims.lastAcquire.ExpectedTaskResourceVersion)
		require.Equal(t, "claim-1", host.claims.lastAcquire.IdempotencyKey)
		statusResponse, err := plugin.HandleAction(ctx, actionRequest("task.status", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		status := readAction[struct {
			Tasks []map[string]any `json:"tasks"`
		}](t, statusResponse)
		require.Equal(t, float64(7), status.Tasks[0]["lastCoordinatorClaimGeneration"])

		response, err = plugin.HandleAction(ctx, actionRequest("completion.criteria", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "task_id": "task-1", "request_id": "criteria-1",
			"criteria": []map[string]any{{"id": "tests-pass", "description": "Regression tests pass", "evidence_kind": "ci_run", "evidence_id": "run-9", "evidence_revision": "head-abc"}},
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Equal(t, int64(7), host.completion.criteria.ExpectedClaimGeneration)

		response, err = plugin.HandleAction(ctx, actionRequest("completion.evidence", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "task_id": "task-1", "criterion_id": "tests-pass", "request_id": "evidence-1",
			"evidence_kind": "ci_run", "evidence_id": "run-9", "evidence_revision": "head-abc", "summary": "CI passed on the reviewed revision", "reference": "https://ci.example/run/9",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Equal(t, "task-v1", host.completion.evidence.ExpectedTaskResourceVersion)
		require.Equal(t, int64(7), host.completion.evidence.ExpectedClaimGeneration)

		host.queries.mu.Lock()
		host.queries.resourceVersion = "task-v2"
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(ctx, actionRequest("completion.evidence", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "task_id": "task-1", "criterion_id": "tests-pass", "request_id": "stale-evidence",
			"evidence_kind": "ci_run", "evidence_id": "run-9", "evidence_revision": "head-abc", "summary": "Stale evidence must be rejected",
		}))
		require.NoError(t, err)
		require.Equal(t, 409, response.Status)
	})

	t.Run("writeback keeps uncertain receipts and uses a separately approved grant", func(t *testing.T) {
		plugin, host := operationalPlugin(t)
		ctx := context.Background()
		response, err := plugin.HandleAction(ctx, actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "delegate-issue-task", "title": "Fix linked issue",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)
		response, err = plugin.HandleAction(ctx, actionRequest("issue.capabilities", "workspace-1", map[string]any{"task_id": "task-1"}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		capabilities := readAction[struct {
			Capabilities struct {
				Identifier  string `json:"identifier"`
				Transitions []struct {
					TargetID string `json:"targetId"`
				} `json:"transitions"`
			} `json:"capabilities"`
			Writeback map[string]map[string]any `json:"writeback"`
		}](t, response)
		require.Equal(t, "ACME-12", capabilities.Capabilities.Identifier)
		require.Equal(t, "in-progress", capabilities.Capabilities.Transitions[0].TargetID)
		require.False(t, capabilities.Writeback["comment"]["authorized"].(bool))

		request := map[string]any{"instance_key": "delivery-lead", "task_id": "task-1", "request_id": "write-1", "body": "The change is ready for review."}
		response, err = plugin.HandleAction(ctx, actionRequest("issue.comment", "workspace-1", request))
		require.NoError(t, err)
		require.Equal(t, 403, response.Status)
		require.Empty(t, host.issues.commentCalls)
		writesBeforeGrant, err := plugin.store.ListSourceWriteStatuses(ctx, "workspace-1", "delivery-lead")
		require.NoError(t, err)
		require.Empty(t, writesBeforeGrant)

		host.writeAuthorized = true
		response, err = plugin.HandleAction(ctx, actionRequest("issue.comment", "workspace-1", request))
		require.NoError(t, err)
		require.Equal(t, 502, response.Status)
		require.Contains(t, string(response.Body), "uncertain")
		response, err = plugin.HandleAction(ctx, actionRequest("outcome.report", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		var uncertainty struct {
			Writes []sourceWriteStatus `json:"uncertainWrites"`
		}
		require.NoError(t, json.Unmarshal(response.Body, &uncertainty))
		require.Len(t, uncertainty.Writes, 1)
		require.Equal(t, "uncertain", uncertainty.Writes[0].Status)

		response, err = plugin.HandleAction(ctx, actionRequest("issue.writes.list", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		writes := readAction[struct {
			Writes []sourceWriteIntent `json:"writes"`
		}](t, response)
		require.Len(t, writes.Writes, 1)
		require.Equal(t, "uncertain", writes.Writes[0].Status)

		response, err = plugin.HandleAction(ctx, actionRequest("issue.write.retry", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "write-1",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		require.Len(t, host.issues.commentCalls, 2)
		require.Equal(t, host.issues.commentCalls[0].IdempotencyKey, host.issues.commentCalls[1].IdempotencyKey)
		require.Equal(t, "source-v3", host.issues.commentCalls[1].ExpectedSourceResourceVersion)

		response, err = plugin.HandleAction(ctx, actionRequest("issue.writes.list", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		writes = readAction[struct {
			Writes []sourceWriteIntent `json:"writes"`
		}](t, response)
		require.Equal(t, "APPLIED", writes.Writes[0].Status)
	})

	t.Run("outcome report distinguishes verification and measured usage", func(t *testing.T) {
		plugin, host := operationalPlugin(t)
		ctx := context.Background()
		response, err := plugin.HandleAction(ctx, actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "delegate-outcome", "title": "Run the release checks",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)

		host.queries.mu.Lock()
		host.queries.canonicalStatus = "completed"
		cost := int64(21000)
		host.queries.usage = &pluginsdk.TaskUsageObservation{CostSubcents: &cost, Currency: "USD", CostComplete: false}
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(ctx, actionRequest("outcome.report", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status, string(response.Body))
		reports := readAction[struct {
			Reports []outcomeReport `json:"reports"`
		}](t, response)
		require.Len(t, reports.Reports, 1)
		require.True(t, reports.Reports[0].Verified)
		require.Equal(t, "estimated", reports.Reports[0].CostState)
		require.InDelta(t, 2.10, *reports.Reports[0].CostUSD, 0.001)
		require.Equal(t, "Host task snapshot task-v1", reports.Reports[0].Provenance)

		host.queries.mu.Lock()
		host.queries.usage.CostComplete = true
		host.queries.blockingReasons = []string{"evidence_stale"}
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(ctx, actionRequest("outcome.report", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		reports = readAction[struct {
			Reports []outcomeReport `json:"reports"`
		}](t, response)
		require.Equal(t, "measured", reports.Reports[0].CostState)
		require.Equal(t, "completed", reports.Reports[0].Status)
		require.False(t, reports.Reports[0].Verified)

		host.queries.mu.Lock()
		host.queries.canonicalStatus = "in_progress"
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(ctx, actionRequest("outcome.report", "workspace-1", map[string]any{"instance_key": "delivery-lead"}))
		require.NoError(t, err)
		reports = readAction[struct {
			Reports []outcomeReport `json:"reports"`
		}](t, response)
		require.Equal(t, "blocked", reports.Reports[0].Status)
		require.False(t, reports.Reports[0].Verified)
	})
}

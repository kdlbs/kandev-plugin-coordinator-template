package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

type coordinatorTestHost struct {
	pluginsdk.UnimplementedHostData
	mu            sync.Mutex
	state         map[string]map[string]any
	conversations *testConversations
	tasks         *testTaskCommands
	queries       *testExactQueries
}

func newCoordinatorTestHost() *coordinatorTestHost {
	return &coordinatorTestHost{
		state:         map[string]map[string]any{},
		conversations: &testConversations{byKey: map[string]pluginsdk.ManagedAgentConversationDescriptor{}},
		tasks:         &testTaskCommands{byExternalID: map[string]pluginsdk.Task{}},
		queries:       &testExactQueries{executionState: "idle"},
	}
}

func newCoordinatorTestPlugin(t *testing.T, host *coordinatorTestHost, dataDir string) *coordinatorPlugin {
	t.Helper()
	store, err := openPolicyStore(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	plugin := &coordinatorPlugin{store: store}
	plugin.SetHost(host)
	return plugin
}

func (*coordinatorTestHost) GetConfig(context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}
func (*coordinatorTestHost) RevealSecret(context.Context, string) (string, error) { return "", nil }
func (*coordinatorTestHost) GetSecret(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (*coordinatorTestHost) SetSecret(context.Context, string, string) error         { return nil }
func (*coordinatorTestHost) DeleteSecret(context.Context, string) error              { return nil }
func (*coordinatorTestHost) EmitEvent(context.Context, string, map[string]any) error { return nil }

func (h *coordinatorTestHost) GetState(_ context.Context, scope, scopeID, key string) (map[string]any, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	value, ok := h.state[scope+"/"+scopeID+"/"+key]
	return value, ok, nil
}

func (h *coordinatorTestHost) SetState(_ context.Context, scope, scopeID, key string, value map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state[scope+"/"+scopeID+"/"+key] = value
	return nil
}

func (h *coordinatorTestHost) DeleteState(_ context.Context, scope, scopeID, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.state, scope+"/"+scopeID+"/"+key)
	return nil
}

func (*coordinatorTestHost) ListState(context.Context, string, string) ([]pluginsdk.StateEntry, error) {
	return nil, nil
}

func (h *coordinatorTestHost) GetCapabilityContext(_ context.Context, workspaceID string) (*pluginsdk.CapabilityContext, error) {
	return &pluginsdk.CapabilityContext{
		InstallationID: "installation-1", WorkspaceID: workspaceID,
		ManifestDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ApprovalRevision: 1, ApprovalState: "active",
		Operations: []pluginsdk.CapabilityOperation{
			{Method: "EnsureManagedAgentConversationExact", CapabilityID: "host.v2.write:managed_agent_conversations", Supported: true, Authorized: true},
			{Method: "GetManagedAgentConversationStatusExact", CapabilityID: "host.v2.read:managed_agent_conversations", Supported: true, Authorized: true},
			{Method: "SetManagedAgentConversationPausedExact", CapabilityID: "host.v2.write:managed_agent_conversations", Supported: true, Authorized: true},
			{Method: "CreateTaskExact", CapabilityID: "host.v2.write:tasks", Supported: true, Authorized: true},
			{Method: "GetTaskExact", CapabilityID: "host.v2.read:tasks", Supported: true, Authorized: true},
			{Method: "ListTaskUsageExact", CapabilityID: "host.v2.read:tasks", Supported: true, Authorized: true},
		},
	}, nil
}

func (h *coordinatorTestHost) UpdateTaskExact(context.Context, pluginsdk.ExactTaskUpdate) (*pluginsdk.CommandResult, *pluginsdk.Task, error) {
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandNoChange}, nil, nil
}

func (h *coordinatorTestHost) ManagedAgentConversations() pluginsdk.ManagedAgentConversationManager {
	return h.conversations
}

func (h *coordinatorTestHost) TaskCommands() pluginsdk.ExactTaskCommandManager { return h.tasks }

func (h *coordinatorTestHost) ExactQueries() pluginsdk.ExactQueryManager { return h.queries }

type testExactQueries struct {
	pluginsdk.ExactQueryManager
	mu              sync.Mutex
	executionState  string
	canonicalStatus string
	resourceVersion string
	blockingReasons []string
	workspaces      []pluginsdk.ExactWorkspaceObservation
	workflows       []pluginsdk.ExactWorkflowObservation
	usage           *pluginsdk.TaskUsageObservation
}

func (q *testExactQueries) ListWorkspaces(_ context.Context, _ pluginsdk.ExactWorkspaceQuery) (pluginsdk.ExactWorkspacePage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return pluginsdk.ExactWorkspacePage{Items: append([]pluginsdk.ExactWorkspaceObservation(nil), q.workspaces...), PageInfo: pluginsdk.ExactReadPageInfo{SnapshotVersion: "workspace-snapshot-1"}}, nil
}

func (q *testExactQueries) ListWorkflows(_ context.Context, _ pluginsdk.ExactWorkflowQuery) (pluginsdk.ExactWorkflowPage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return pluginsdk.ExactWorkflowPage{Items: append([]pluginsdk.ExactWorkflowObservation(nil), q.workflows...), PageInfo: pluginsdk.ExactReadPageInfo{SnapshotVersion: "workflow-snapshot-1"}}, nil
}

func (q *testExactQueries) GetTask(_ context.Context, input pluginsdk.ExactTaskGetQuery) (pluginsdk.ExactTaskObservation, pluginsdk.HostReadReceipt, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	status := q.canonicalStatus
	if status == "" {
		status = "todo"
	}
	version := q.resourceVersion
	if version == "" {
		version = "task-v1"
	}
	return pluginsdk.ExactTaskObservation{
		Task: pluginsdk.Task{ID: input.TaskID, WorkspaceID: input.WorkspaceID}, StatusKnown: true,
		ExecutionState: q.executionState, CanonicalStatus: status, ResourceVersion: version,
		BlockingReasons: append([]string(nil), q.blockingReasons...),
	}, pluginsdk.HostReadReceipt{WorkspaceID: input.WorkspaceID, SnapshotVersion: "snapshot-1"}, nil
}

func (q *testExactQueries) ListTaskUsage(_ context.Context, input pluginsdk.ExactTaskUsageQuery) (pluginsdk.ExactTaskUsagePage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	page := pluginsdk.ExactTaskUsagePage{PageInfo: pluginsdk.ExactReadPageInfo{SnapshotVersion: "snapshot-1"}}
	if q.usage != nil {
		for _, taskID := range input.TaskIDs {
			item := *q.usage
			item.TaskID = taskID
			page.Items = append(page.Items, item)
		}
	}
	return page, nil
}

type testConversations struct {
	pluginsdk.ManagedAgentConversationManager
	mu    sync.Mutex
	byKey map[string]pluginsdk.ManagedAgentConversationDescriptor
	specs []pluginsdk.ManagedAgentConversationSpec
}

func (m *testConversations) Ensure(_ context.Context, spec pluginsdk.ManagedAgentConversationSpec) (*pluginsdk.CommandResult, pluginsdk.ManagedAgentConversationDescriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := spec.WorkspaceID + "/" + spec.InstanceKey
	descriptor, exists := m.byKey[key]
	if exists && spec.ExpectedRevision != descriptor.Revision {
		return &pluginsdk.CommandResult{Status: pluginsdk.CommandConflict, Reason: "stale_revision"}, descriptor, nil
	}
	descriptor = pluginsdk.ManagedAgentConversationDescriptor{
		InstallationID: "installation-1", WorkspaceID: spec.WorkspaceID, InstanceKey: spec.InstanceKey,
		TaskID: "managed-" + spec.InstanceKey, SessionID: "session-" + spec.InstanceKey,
		Revision: spec.ExpectedRevision + 1, AgentProfileID: spec.AgentProfileID,
		ExecutorID: spec.ExecutorID, ExecutorProfileID: spec.ExecutorProfileID,
		BasePrompt: spec.BasePrompt, InstructionVersion: spec.InstructionVersion,
		AgentToolNames: append([]string(nil), spec.AgentToolNames...), RetentionMode: "retain_on_uninstall",
	}
	m.byKey[key] = descriptor
	m.specs = append(m.specs, spec)
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, descriptor, nil
}

func (m *testConversations) Get(_ context.Context, query pluginsdk.ManagedAgentConversationQuery) (pluginsdk.ManagedAgentConversationDescriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byKey[query.WorkspaceID+"/"+query.InstanceKey], nil
}

func (m *testConversations) SetPaused(_ context.Context, input pluginsdk.ManagedAgentConversationPause) (*pluginsdk.CommandResult, pluginsdk.ManagedAgentConversationDescriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := input.WorkspaceID + "/" + input.InstanceKey
	descriptor := m.byKey[key]
	descriptor.Revision = input.ExpectedRevision + 1
	descriptor.DesiredPaused = input.Paused
	m.byKey[key] = descriptor
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, descriptor, nil
}

type testTaskCommands struct {
	pluginsdk.ExactTaskCommandManager
	mu           sync.Mutex
	byExternalID map[string]pluginsdk.Task
	createdRows  int
}

func (m *testTaskCommands) CreateTask(_ context.Context, input pluginsdk.ExactTaskCreate) (*pluginsdk.CommandResult, *pluginsdk.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.byExternalID[input.ExternalID]; ok {
		return &pluginsdk.CommandResult{Status: pluginsdk.CommandAlreadyApplied}, &task, nil
	}
	task := pluginsdk.Task{ID: "task-1", WorkspaceID: input.WorkspaceID, Title: input.Task.Title, State: "todo"}
	m.byExternalID[input.ExternalID] = task
	m.createdRows++
	return &pluginsdk.CommandResult{Status: pluginsdk.CommandApplied}, &task, nil
}

func actionRequest(key, workspaceID string, body any) *pluginsdk.PluginActionRequest {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return &pluginsdk.PluginActionRequest{
		ActionKey: key, Context: pluginsdk.VerifiedActionContext{WorkspaceID: workspaceID}, Body: encoded,
	}
}

func TestCoordinatorInstancesKeepProfilesAndMemorySeparateAcrossRestart(t *testing.T) {
	host := newCoordinatorTestHost()
	dataDir := t.TempDir()
	plugin := newCoordinatorTestPlugin(t, host, dataDir)

	for _, body := range []map[string]any{
		{"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast", "instructions": "Track delivery risks."},
		{"instance_key": "reviewer", "name": "Reviewer", "role": "code-reviewer", "agent_profile_id": "profile-careful", "instructions": "Check evidence before approval."},
	} {
		response, err := plugin.HandleAction(context.Background(), actionRequest("instance.save", "workspace-1", body))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)
	}

	list, err := plugin.HandleAction(context.Background(), actionRequest("instance.list", "workspace-1", map[string]any{}))
	require.NoError(t, err)
	var listBody struct {
		Instances []coordinatorInstance `json:"instances"`
	}
	require.NoError(t, json.Unmarshal(list.Body, &listBody))
	require.Len(t, listBody.Instances, 2)
	require.NotEqual(t, listBody.Instances[0].AgentProfileID, listBody.Instances[1].AgentProfileID)

	toolRequest := func(invocation, instanceKey, value string) *pluginsdk.AgentToolRequest {
		return &pluginsdk.AgentToolRequest{
			InvocationID: invocation, Name: "coordinator_remember",
			Context: pluginsdk.AgentToolContext{
				WorkspaceID: "workspace-1", TaskID: "managed-" + instanceKey, SessionID: "session-" + instanceKey,
				Surface: "managed-conversation", InstallationID: "installation-1",
				ConversationRevision: 1, ApprovalRevision: 1,
				ManifestDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				AgentToolNames: []string{"coordinator_remember"},
			},
			Arguments: map[string]any{"instance_key": instanceKey, "key": "review-focus", "value": value},
		}
	}
	_, err = plugin.InvokeAgentTool(context.Background(), toolRequest("remember-1", "delivery-lead", "release risks"))
	require.NoError(t, err)
	_, err = plugin.InvokeAgentTool(context.Background(), toolRequest("remember-2", "reviewer", "test evidence"))
	require.NoError(t, err)

	// A restarted plugin reads the same durable instance and memory rows.
	require.NoError(t, plugin.store.Close())
	restartedStore, err := openPolicyStore(dataDir)
	require.NoError(t, err)
	defer restartedStore.Close()
	restarted := &coordinatorPlugin{store: restartedStore}
	restarted.SetHost(host)
	for instanceKey, want := range map[string]string{"delivery-lead": "release risks", "reviewer": "test evidence"} {
		got, err := restarted.readMemory(context.Background(), "workspace-1", instanceKey, "review-focus")
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	wrongConversation := toolRequest("wrong-owner", "reviewer", "must not be stored")
	wrongConversation.Context.TaskID = "managed-delivery-lead"
	_, err = restarted.InvokeAgentTool(context.Background(), wrongConversation)
	require.NoError(t, err)
	got, err := restarted.readMemory(context.Background(), "workspace-1", "reviewer", "review-focus")
	require.NoError(t, err)
	require.Equal(t, "test evidence", got)
}

func TestCoordinatorAgentToolDelegationUsesHostIdempotency(t *testing.T) {
	host := newCoordinatorTestHost()
	plugin := newCoordinatorTestPlugin(t, host, t.TempDir())
	_, err := plugin.HandleAction(context.Background(), actionRequest("instance.save", "workspace-1", map[string]any{
		"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast",
	}))
	require.NoError(t, err)

	request := &pluginsdk.AgentToolRequest{
		InvocationID: "delegate-once", Name: "coordinator_create_task",
		Context: pluginsdk.AgentToolContext{
			WorkspaceID: "workspace-1", TaskID: "managed-delivery-lead", SessionID: "session-delivery-lead",
			Surface: "managed-conversation", InstallationID: "installation-1",
			ConversationRevision: 1, ApprovalRevision: 1,
			ManifestDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AgentToolNames: []string{"coordinator_create_task"},
		},
		Arguments: map[string]any{"instance_key": "delivery-lead", "title": "Verify release notes", "description": "Check the final release notes."},
	}
	first, err := plugin.InvokeAgentTool(context.Background(), request)
	require.NoError(t, err)
	second, err := plugin.InvokeAgentTool(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.StructuredContent["task_id"], second.StructuredContent["task_id"])
	require.Equal(t, 1, host.tasks.createdRows)
	list, err := plugin.HandleAction(context.Background(), actionRequest("instance.list", "workspace-1", map[string]any{}))
	require.NoError(t, err)
	var result struct {
		Instances []coordinatorInstance `json:"instances"`
	}
	require.NoError(t, json.Unmarshal(list.Body, &result))
	require.Equal(t, []string{"task-1"}, result.Instances[0].TaskIDs)
}

func TestCoordinatorPolicyLimits(t *testing.T) {
	t.Run("active runs", func(t *testing.T) {
		host := newCoordinatorTestHost()
		plugin := newCoordinatorTestPlugin(t, host, t.TempDir())
		response, err := plugin.HandleAction(context.Background(), actionRequest("instance.save", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast", "max_concurrent_runs": 1,
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)
		response, err = plugin.HandleAction(context.Background(), actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "first", "title": "First task",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)

		host.queries.mu.Lock()
		host.queries.executionState = "running"
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(context.Background(), actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "second", "title": "Second task",
		}))
		require.NoError(t, err)
		require.Equal(t, 409, response.Status)
		require.Contains(t, string(response.Body), "concurrency_limit")
		require.Equal(t, 1, host.tasks.createdRows)
	})

	t.Run("measured budget", func(t *testing.T) {
		host := newCoordinatorTestHost()
		plugin := newCoordinatorTestPlugin(t, host, t.TempDir())
		response, err := plugin.HandleAction(context.Background(), actionRequest("instance.save", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast",
			"estimated_budget_usd": 1, "max_concurrent_runs": 4,
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)
		response, err = plugin.HandleAction(context.Background(), actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "first", "title": "First task",
		}))
		require.NoError(t, err)
		require.Equal(t, 200, response.Status)

		cost := int64(10000)
		host.queries.mu.Lock()
		host.queries.usage = &pluginsdk.TaskUsageObservation{CostSubcents: &cost, Currency: "USD", CostComplete: true}
		host.queries.mu.Unlock()
		response, err = plugin.HandleAction(context.Background(), actionRequest("task.delegate", "workspace-1", map[string]any{
			"instance_key": "delivery-lead", "request_id": "second", "title": "Second task",
		}))
		require.NoError(t, err)
		require.Equal(t, 409, response.Status)
		require.Contains(t, string(response.Body), "budget_limit")
		require.Equal(t, 1, host.tasks.createdRows)
	})
}

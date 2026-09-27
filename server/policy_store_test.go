package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyStoreAddsSourceWriteIntentIdentityColumn(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "coordinator.db")
	db, err := sql.Open("sqlite", databasePath)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE source_write_intents (
		workspace_id TEXT NOT NULL, instance_key TEXT NOT NULL, task_id TEXT NOT NULL,
		request_id TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE, payload_json TEXT NOT NULL,
		status TEXT NOT NULL, receipt_json TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		PRIMARY KEY (workspace_id, instance_key, request_id))`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := openPolicyStore(filepath.Dir(databasePath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	rows, err := store.db.QueryContext(ctx, `PRAGMA table_info(source_write_intents)`)
	require.NoError(t, err)
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		require.NoError(t, rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey))
		found = found || name == "request_json"
	}
	require.NoError(t, rows.Err())
	require.True(t, found)
}

func TestPolicyStoreScopesInstancesMemoryAndRoles(t *testing.T) {
	ctx := context.Background()
	store, err := openPolicyStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceInstances(ctx, "workspace-one", []coordinatorInstance{{
		Key: "delivery-lead", Name: "Delivery lead", Role: "delivery-lead", TaskIDs: []string{"task-one"},
	}}))
	require.NoError(t, store.ReplaceInstances(ctx, "workspace-two", []coordinatorInstance{{
		Key: "delivery-lead", Name: "Separate lead", Role: "delivery-lead", TaskIDs: []string{"task-two"},
	}}))
	require.NoError(t, store.SaveMemory(ctx, "workspace-one", "delivery-lead", "focus", "workspace one"))
	require.NoError(t, store.SaveMemory(ctx, "workspace-two", "delivery-lead", "focus", "workspace two"))

	first, err := store.ListInstances(ctx, "workspace-one")
	require.NoError(t, err)
	require.Equal(t, "task-one", first[0].TaskIDs[0])
	second, err := store.ListInstances(ctx, "workspace-two")
	require.NoError(t, err)
	require.Equal(t, "task-two", second[0].TaskIDs[0])
	firstMemory, found, err := store.ReadMemory(ctx, "workspace-one", "delivery-lead", "focus")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "workspace one", firstMemory)
	secondMemory, found, err := store.ReadMemory(ctx, "workspace-two", "delivery-lead", "focus")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "workspace two", secondMemory)

	roles, err := store.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 4)
	require.Equal(t, "code-reviewer", roles[1].Key)
	require.Contains(t, roles[1].DefaultAgentToolNames, rememberTool)
}

func TestProposalOutboxRecovery(t *testing.T) {
	ctx := context.Background()
	host := newCoordinatorTestHost()
	dataDir := t.TempDir()
	plugin := newCoordinatorTestPlugin(t, host, dataDir)
	response, err := plugin.HandleAction(ctx, actionRequest("instance.save", "workspace-one", map[string]any{
		"instance_key": "delivery-lead", "name": "Delivery lead", "role": "chief-of-staff", "agent_profile_id": "profile-fast",
	}))
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)

	response, err = plugin.HandleAction(ctx, actionRequest("proposal.create", "workspace-one", map[string]any{
		"instance_key": "delivery-lead", "source_id": "event-17", "title": "Review the release", "description": "Check the release notes.",
	}))
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)
	var created struct {
		Proposal  proposalRecord `json:"proposal"`
		Duplicate bool           `json:"duplicate"`
	}
	require.NoError(t, json.Unmarshal(response.Body, &created))
	require.False(t, created.Duplicate)

	store := plugin.store
	proposal, outbox, duplicate, err := store.ApproveProposal(ctx, "workspace-one", created.Proposal.ProposalID, created.Proposal.Revision)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, "approved", proposal.Approval)
	require.Equal(t, "pending", outbox.Status)

	// Simulate a crash after the Host effect but before the plugin records its receipt.
	var task taskRequest
	require.NoError(t, json.Unmarshal([]byte(outbox.PayloadJSON), &task))
	task.InstanceKey = outbox.InstanceKey
	task.RequestID = outbox.IdempotencyKey
	firstEffect, err := plugin.createDelegatedTask(ctx, "workspace-one", task)
	require.NoError(t, err)
	require.Equal(t, 200, firstEffect.Status)
	require.NoError(t, store.Close())

	restartedStore, err := openPolicyStore(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restartedStore.Close() })
	restarted := &coordinatorPlugin{store: restartedStore}
	restarted.SetHost(host)
	response, err = restarted.HandleAction(ctx, actionRequest("proposal.reconcile", "workspace-one", map[string]any{"instance_key": "delivery-lead"}))
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)
	require.Equal(t, 1, host.tasks.createdRows)

	var reconciliation struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(response.Body, &reconciliation))
	require.Len(t, reconciliation.Results, 1)
	require.Equal(t, "completed", reconciliation.Results[0]["status"])

	response, err = restarted.HandleAction(ctx, actionRequest("proposal.approve", "workspace-one", map[string]any{
		"proposal_id": created.Proposal.ProposalID, "revision": created.Proposal.Revision,
	}))
	require.NoError(t, err)
	require.Equal(t, 200, response.Status)
	require.Equal(t, 1, host.tasks.createdRows)
	var receipt struct {
		Status string `json:"status"`
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(response.Body, &receipt))
	require.Equal(t, "completed", receipt.Status)
	require.Equal(t, "task-1", receipt.TaskID)
}

func TestProposalSourceDeduplicationAndApprovalCAS(t *testing.T) {
	ctx := context.Background()
	store, err := openPolicyStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	first, duplicate, err := store.CreateProposal(ctx, "workspace-one", "lead", "", "event-1", `{"title":"Review"}`)
	require.NoError(t, err)
	require.False(t, duplicate)
	second, duplicate, err := store.CreateProposal(ctx, "workspace-one", "lead", "", "event-1", `{"title":"Different replay"}`)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, first.ProposalID, second.ProposalID)
	require.Equal(t, first.InputJSON, second.InputJSON)

	_, _, duplicate, err = store.ApproveProposal(ctx, "workspace-one", first.ProposalID, first.Revision)
	require.NoError(t, err)
	require.False(t, duplicate)
	_, outbox, duplicate, err := store.ApproveProposal(ctx, "workspace-one", first.ProposalID, first.Revision)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.NotEmpty(t, outbox.IdempotencyKey)
}

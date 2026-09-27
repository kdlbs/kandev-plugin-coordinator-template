package main

import (
	"context"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorEventReconciliationIsFilteredDebouncedAndDurable(t *testing.T) {
	ctx := context.Background()
	host := newCoordinatorTestHost()
	dataDir := t.TempDir()
	plugin := newCoordinatorTestPlugin(t, host, dataDir)
	store := plugin.store
	require.NoError(t, store.ReplaceInstances(ctx, "workspace-one", []coordinatorInstance{{
		Key: "lead", Name: "Lead", Role: "chief-of-staff", TaskIDs: []string{"task-one"}, MaxConcurrentRuns: 2,
	}}))
	require.NoError(t, store.SetTaskWatch(ctx, taskWatchRecord{
		WorkspaceID: "workspace-one", InstanceKey: "lead", TaskID: "task-one",
		Filter: []string{"task.updated", "task.state_changed"}, DebounceSeconds: 60,
		MaxFollowupDepth: 1, LastTaskRevision: "before-event",
	}))

	baseEvent := func(id, eventType string) *pluginsdk.Event {
		return &pluginsdk.Event{EventID: id, EventType: eventType, WorkspaceID: "workspace-one", Payload: map[string]any{"task_id": "task-one"}}
	}
	require.NoError(t, plugin.OnEvent(ctx, baseEvent("evt-one", "task.updated")))
	require.NoError(t, plugin.OnEvent(ctx, baseEvent("evt-one", "task.updated")))
	require.NoError(t, plugin.OnEvent(ctx, baseEvent("evt-two", "task.state_changed")))
	require.NoError(t, plugin.OnEvent(ctx, baseEvent("evt-filtered", "task.created")))

	proposals, err := store.ListProposals(ctx, "workspace-one", "lead", 50)
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	require.Equal(t, "pending", proposals[0].Approval)
	require.Contains(t, proposals[0].SourceID, "evt-one")

	// Restart keeps receipts and the stable event cursor, so a redelivery cannot add work.
	require.NoError(t, store.Close())
	restartedStore, err := openPolicyStore(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = restartedStore.Close() })
	restarted := &coordinatorPlugin{store: restartedStore}
	restarted.SetHost(host)
	require.NoError(t, restarted.OnEvent(ctx, baseEvent("evt-one", "task.updated")))
	proposals, err = restartedStore.ListProposals(ctx, "workspace-one", "lead", 50)
	require.NoError(t, err)
	require.Len(t, proposals, 1)

	var cursor string
	require.NoError(t, restartedStore.db.QueryRowContext(ctx, `SELECT cursor FROM event_cursors WHERE workspace_id = ? AND instance_key = ? AND source = 'task_events'`, "workspace-one", "lead").Scan(&cursor))
	require.NotEmpty(t, cursor)
}

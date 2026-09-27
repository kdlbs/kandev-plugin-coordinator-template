package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

var coordinatorWatchEvents = []string{"task.updated", "task.state_changed"}

type watchSetRequest struct {
	InstanceKey      string   `json:"instance_key"`
	TaskID           string   `json:"task_id"`
	Events           []string `json:"events"`
	DebounceSeconds  int      `json:"debounce_seconds"`
	MaxFollowupDepth int      `json:"max_followup_depth"`
}

func (p *coordinatorPlugin) setTaskWatch(ctx context.Context, workspaceID string, input watchSetRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if instance.Paused {
		return actionError(http.StatusConflict, "paused instances cannot change task watches"), nil
	}
	if input.TaskID == "" || !contains(instance.TaskIDs, input.TaskID) {
		return actionError(http.StatusBadRequest, "task must be linked to this coordinator instance"), nil
	}
	if input.DebounceSeconds < 0 || input.DebounceSeconds > 86400 || input.MaxFollowupDepth < 0 || input.MaxFollowupDepth > 10 {
		return actionError(http.StatusBadRequest, "watch debounce or follow-up depth is outside its limit"), nil
	}
	filter := input.Events
	if len(filter) == 0 {
		filter = append([]string(nil), coordinatorWatchEvents...)
	}
	for _, eventType := range filter {
		if eventType != "task.updated" && eventType != "task.state_changed" {
			return actionError(http.StatusBadRequest, "task watch supports task.updated and task.state_changed"), nil
		}
	}
	revision, err := p.readTaskRevision(ctx, workspaceID, input.TaskID)
	if err != nil {
		return actionError(http.StatusServiceUnavailable, "Host task status is unavailable"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	watch := taskWatchRecord{
		WorkspaceID: workspaceID, InstanceKey: instance.Key, TaskID: input.TaskID,
		Filter: filter, DebounceSeconds: input.DebounceSeconds,
		MaxFollowupDepth: input.MaxFollowupDepth, LastTaskRevision: revision,
	}
	if err := store.SetTaskWatch(ctx, watch); err != nil {
		return actionError(http.StatusInternalServerError, "task watch could not be saved"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"watch": watch})
}

func (p *coordinatorPlugin) listTaskWatches(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	if _, err := p.getInstance(ctx, workspaceID, instanceKey); err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	watches, err := store.ListTaskWatches(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "task watches are unavailable"), nil
	}
	return actionJSON(http.StatusOK, map[string]any{"watches": watches})
}

func (p *coordinatorPlugin) reconcileTaskWatches(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	store, err := p.policyStore()
	if err != nil {
		return actionError(http.StatusInternalServerError, "coordinator policy storage is unavailable"), nil
	}
	watches, err := store.ListTaskWatches(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusInternalServerError, "task watches are unavailable"), nil
	}
	if instance.Paused {
		return actionJSON(http.StatusOK, map[string]any{"status": "paused", "reconciled": 0})
	}
	policy, err := p.coordinatorPolicy(ctx, workspaceID, instance)
	if err != nil {
		return actionError(http.StatusServiceUnavailable, "canonical task status or usage is unavailable"), nil
	}
	if policy.Blocked {
		return actionJSON(http.StatusOK, map[string]any{"status": "policy_held", "reconciled": 0, "policy": policy})
	}
	results := make([]map[string]any, 0, len(watches))
	for _, watch := range watches {
		observation, err := p.readTaskObservation(ctx, workspaceID, watch.TaskID)
		if err != nil {
			return actionError(http.StatusServiceUnavailable, "canonical task status is unavailable"), nil
		}
		if observation.ResourceVersion == "" || observation.ResourceVersion == watch.LastTaskRevision {
			results = append(results, map[string]any{"task_id": watch.TaskID, "status": "current"})
			continue
		}
		eventID := "snapshot:" + watch.TaskID + ":" + observation.ResourceVersion
		_, result, duplicate, err := store.ProcessWatchedTaskEvent(ctx, workspaceID, instanceKey, watch.TaskID,
			eventID, "task.updated", observation.ResourceVersion, observation.Task.Title, "")
		if err != nil {
			return actionError(http.StatusInternalServerError, "task snapshot could not be reconciled"), nil
		}
		results = append(results, map[string]any{"task_id": watch.TaskID, "status": result, "duplicate": duplicate})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": "reconciled", "results": results})
}

func (p *coordinatorPlugin) OnEvent(ctx context.Context, event *pluginsdk.Event) error {
	if event == nil {
		return nil
	}
	if event.EventType != "task.created" && event.EventType != "task.updated" && event.EventType != "task.state_changed" {
		return nil
	}
	if event.EventID == "" || event.WorkspaceID == "" {
		return errors.New("coordinator task event is missing its identity or workspace")
	}
	taskID := stringFromEvent(event.Payload["task_id"])
	if taskID == "" {
		taskID = stringFromEvent(event.Payload["taskId"])
	}
	if taskID == "" {
		return errors.New("coordinator task event is missing its task id")
	}
	store, err := p.policyStore()
	if err != nil {
		return err
	}
	watches, err := store.ListTaskWatchesForTask(ctx, event.WorkspaceID, taskID)
	if err != nil || len(watches) == 0 {
		return err
	}
	observation, err := p.readTaskObservation(ctx, event.WorkspaceID, taskID)
	if err != nil {
		return err
	}
	for _, watch := range watches {
		instance, err := p.getInstance(ctx, event.WorkspaceID, watch.InstanceKey)
		if err != nil {
			continue
		}
		holdReason := ""
		if instance.Paused {
			holdReason = "paused"
		} else {
			policy, policyErr := p.coordinatorPolicy(ctx, event.WorkspaceID, instance)
			if policyErr != nil {
				return policyErr
			}
			if policy.Blocked {
				holdReason = "policy_held"
			}
		}
		_, _, _, err = store.ProcessWatchedTaskEvent(ctx, event.WorkspaceID, watch.InstanceKey, taskID,
			event.EventID, event.EventType, observation.ResourceVersion, observation.Task.Title, holdReason)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *coordinatorPlugin) readTaskRevision(ctx context.Context, workspaceID, taskID string) (string, error) {
	observation, err := p.readTaskObservation(ctx, workspaceID, taskID)
	if err != nil {
		return "", err
	}
	return observation.ResourceVersion, nil
}

func (p *coordinatorPlugin) readTaskObservation(ctx context.Context, workspaceID, taskID string) (pluginsdk.ExactTaskObservation, error) {
	host := p.Host()
	queries, ok := pluginsdk.HostExactQueries(host)
	if !ok {
		return pluginsdk.ExactTaskObservation{}, errors.New("exact task observations are unavailable")
	}
	if _, _, unavailable := p.exactCapability(ctx, host, workspaceID, "GetTaskExact"); unavailable != nil {
		return pluginsdk.ExactTaskObservation{}, errors.New("exact task observation is not authorized")
	}
	observation, _, err := queries.GetTask(ctx, pluginsdk.ExactTaskGetQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, TaskID: taskID,
	})
	if err != nil {
		return pluginsdk.ExactTaskObservation{}, err
	}
	return observation, nil
}

func (p *coordinatorPlugin) installTaskWatch(ctx context.Context, workspaceID, instanceKey, taskID string, followupDepth int) error {
	if followupDepth < 0 || followupDepth > 10 {
		return fmt.Errorf("invalid task follow-up depth")
	}
	revision, err := p.readTaskRevision(ctx, workspaceID, taskID)
	if err != nil {
		return err
	}
	store, err := p.policyStore()
	if err != nil {
		return err
	}
	return store.SetTaskWatch(ctx, taskWatchRecord{
		WorkspaceID: workspaceID, InstanceKey: instanceKey, TaskID: taskID,
		Filter: append([]string(nil), coordinatorWatchEvents...), DebounceSeconds: 60,
		MaxFollowupDepth: 1, FollowupDepth: followupDepth, LastTaskRevision: revision,
	})
}

func stringFromEvent(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

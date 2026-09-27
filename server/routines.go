package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

type routineCreateRequest struct {
	InstanceKey string                                         `json:"instance_key"`
	RequestID   string                                         `json:"request_id"`
	Name        string                                         `json:"name"`
	Description string                                         `json:"description"`
	Prompt      string                                         `json:"prompt"`
	Triggers    []pluginsdk.ManagedConversationScheduleTrigger `json:"triggers"`
}

type routineEnableRequest struct {
	InstanceKey         string `json:"instance_key"`
	ScheduleID          string `json:"schedule_id"`
	RequestID           string `json:"request_id"`
	ExpectedResourceRev uint64 `json:"expected_resource_revision"`
	Enabled             bool   `json:"enabled"`
}

type routineDeleteRequest struct {
	InstanceKey         string `json:"instance_key"`
	ScheduleID          string `json:"schedule_id"`
	RequestID           string `json:"request_id"`
	ExpectedResourceRev uint64 `json:"expected_resource_revision"`
}

type routineOwnershipResponse struct {
	Schedules []struct {
		ID               string `json:"id"`
		ResourceRevision uint64 `json:"resourceRevision"`
	} `json:"schedules"`
}

func (p *coordinatorPlugin) listRoutines(ctx context.Context, workspaceID, instanceKey string) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, instanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "ListManagedConversationSchedulesExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostManagedConversationSchedules(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "managed conversation schedules are unavailable"), nil
	}
	schedules, err := manager.List(ctx, pluginsdk.ManagedConversationScheduleQuery{
		WorkspaceID: workspaceID, ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not list coordinator schedules"), nil
	}
	owned := make([]map[string]any, 0, len(schedules))
	for _, schedule := range schedules {
		if schedule.InstanceKey == instance.Key {
			owned = append(owned, scheduleDTO(schedule))
		}
	}
	return actionJSON(http.StatusOK, map[string]any{"schedules": owned})
}

func (p *coordinatorPlugin) createRoutine(ctx context.Context, workspaceID string, input routineCreateRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if strings.TrimSpace(input.RequestID) == "" || strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || len(input.Prompt) == 0 || len(input.Prompt) > 20000 || len(input.Triggers) == 0 {
		return actionError(http.StatusBadRequest, "routine needs a request id, name, prompt, and at least one trigger"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "CreateManagedConversationScheduleExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostManagedConversationSchedules(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "managed conversation schedules are unavailable"), nil
	}
	command, schedule, err := manager.Create(ctx, pluginsdk.ManagedConversationScheduleCreate{
		RequestID: input.RequestID, IdempotencyKey: input.RequestID,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
		Schedule: pluginsdk.ManagedConversationSchedule{
			WorkspaceID: workspaceID, Name: strings.TrimSpace(input.Name), Description: input.Description,
			Prompt: input.Prompt, PluginID: "kandev-plugin-coordinator-template", InstanceKey: instance.Key,
			DestinationRevision: instance.ConversationRevision, Enabled: !instance.Paused, Triggers: input.Triggers,
		},
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not create the coordinator routine"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "schedule": scheduleDTO(schedule)})
}

func (p *coordinatorPlugin) setRoutineEnabled(ctx context.Context, workspaceID string, input routineEnableRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if input.RequestID == "" || input.ScheduleID == "" || input.ExpectedResourceRev == 0 {
		return actionError(http.StatusBadRequest, "schedule, request identity, and current schedule revision are required"), nil
	}
	if input.Enabled && instance.Paused {
		return actionError(http.StatusConflict, "paused instances cannot enable routines"), nil
	}
	if !p.ownsRoutineRevision(ctx, workspaceID, instance.Key, input.ScheduleID, input.ExpectedResourceRev) {
		return actionError(http.StatusConflict, "schedule changed or does not belong to this instance"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "SetManagedConversationScheduleEnabledExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostManagedConversationSchedules(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "managed conversation schedules are unavailable"), nil
	}
	command, schedule, err := manager.SetEnabled(ctx, pluginsdk.ManagedConversationScheduleEnabled{
		RequestID: input.RequestID, IdempotencyKey: input.RequestID, WorkspaceID: workspaceID,
		AutomationID: input.ScheduleID, ExpectedResourceRevision: input.ExpectedResourceRev, Enabled: input.Enabled,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not update routine state"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status), "schedule": scheduleDTO(schedule)})
}

func (p *coordinatorPlugin) deleteRoutine(ctx context.Context, workspaceID string, input routineDeleteRequest) (*pluginsdk.PluginActionResponse, error) {
	instance, err := p.getInstance(ctx, workspaceID, input.InstanceKey)
	if err != nil {
		return actionError(http.StatusNotFound, "coordinator instance not found"), nil
	}
	if input.RequestID == "" || input.ScheduleID == "" || input.ExpectedResourceRev == 0 {
		return actionError(http.StatusBadRequest, "schedule, request identity, and current schedule revision are required"), nil
	}
	if !p.ownsRoutineRevision(ctx, workspaceID, instance.Key, input.ScheduleID, input.ExpectedResourceRev) {
		return actionError(http.StatusConflict, "schedule changed or does not belong to this instance"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, "DeleteManagedConversationScheduleExact")
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostManagedConversationSchedules(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "managed conversation schedules are unavailable"), nil
	}
	command, err := manager.Delete(ctx, pluginsdk.ManagedConversationScheduleDelete{
		RequestID: input.RequestID, IdempotencyKey: input.RequestID, WorkspaceID: workspaceID,
		AutomationID: input.ScheduleID, ExpectedResourceRevision: input.ExpectedResourceRev,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	})
	if err != nil {
		return actionError(http.StatusBadGateway, "Host could not delete the coordinator routine"), nil
	}
	if !commandSucceeded(command) {
		return actionJSON(commandHTTPStatus(command.Status), commandEnvelope{Status: string(command.Status), Reason: command.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(command.Status)})
}

func (p *coordinatorPlugin) ownsRoutineRevision(ctx context.Context, workspaceID, instanceKey, scheduleID string, revision uint64) bool {
	list, err := p.listRoutines(ctx, workspaceID, instanceKey)
	if err != nil || list.Status < 200 || list.Status >= 300 {
		return false
	}
	var schedules routineOwnershipResponse
	if json.Unmarshal(list.Body, &schedules) != nil {
		return false
	}
	for _, schedule := range schedules.Schedules {
		if schedule.ID == scheduleID && schedule.ResourceRevision == revision {
			return true
		}
	}
	return false
}

func scheduleDTO(schedule pluginsdk.ManagedConversationSchedule) map[string]any {
	return map[string]any{
		"id": schedule.ID, "workspaceId": schedule.WorkspaceID, "name": schedule.Name,
		"description": schedule.Description, "prompt": schedule.Prompt, "pluginId": schedule.PluginID,
		"instanceKey": schedule.InstanceKey, "destinationRevision": schedule.DestinationRevision,
		"resourceRevision": schedule.ResourceRevision, "enabled": schedule.Enabled, "triggers": schedule.Triggers,
	}
}

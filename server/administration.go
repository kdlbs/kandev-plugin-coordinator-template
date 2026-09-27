package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

type workspaceAdminActionRequest struct {
	Operation                        string  `json:"operation"`
	RequestID                        string  `json:"request_id"`
	IdempotencyKey                   string  `json:"idempotency_key"`
	ExpectedWorkspaceResourceVersion string  `json:"expected_workspace_resource_version"`
	WorkflowID                       string  `json:"workflow_id"`
	ExpectedResourceVersion          string  `json:"expected_resource_version"`
	Name                             *string `json:"name"`
	Description                      *string `json:"description"`
	Prompt                           *string `json:"prompt"`
}

func (p *coordinatorPlugin) workspaceAdminCatalog(ctx context.Context, workspaceID string) (*pluginsdk.PluginActionResponse, error) {
	host := p.Host()
	if _, _, result := p.exactCapability(ctx, host, workspaceID, "ListWorkspacesExact"); result != nil {
		return result, nil
	}
	_, capability, result := p.exactCapability(ctx, host, workspaceID, "ListWorkflowsExact")
	if result != nil {
		return result, nil
	}
	queries, ok := pluginsdk.HostExactQueries(host)
	if !ok {
		return actionError(http.StatusNotImplemented, "workspace catalog queries are unavailable"), nil
	}
	workspaces, err := queries.ListWorkspaces(ctx, pluginsdk.ExactWorkspaceQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, Page: pluginsdk.ExactReadPage{Limit: 100},
	})
	if err != nil || workspaces.PageInfo.HasMore {
		return actionError(http.StatusServiceUnavailable, "workspace catalog is incomplete"), nil
	}
	var workspace *pluginsdk.ExactWorkspaceObservation
	for index := range workspaces.Items {
		if workspaces.Items[index].Workspace.ID == workspaceID {
			workspace = &workspaces.Items[index]
			break
		}
	}
	if workspace == nil {
		return actionError(http.StatusNotFound, "workspace was not present in the approved catalog"), nil
	}
	workflows, err := queries.ListWorkflows(ctx, pluginsdk.ExactWorkflowQuery{
		RequestID: newRequestIDOrEmpty(), WorkspaceID: workspaceID, Page: pluginsdk.ExactReadPage{Limit: 100},
	})
	if err != nil || workflows.PageInfo.HasMore {
		return actionError(http.StatusServiceUnavailable, "workflow catalog is incomplete"), nil
	}
	items := make([]map[string]any, 0, len(workflows.Items))
	for _, item := range workflows.Items {
		items = append(items, map[string]any{
			"id": item.Workflow.ID, "name": item.Workflow.Name, "description": item.Workflow.Description,
			"resourceVersion": item.ResourceVersion,
		})
	}
	createAccess := capabilityMethodStatus(capability, "CreateWorkflowExact")
	updateAccess := capabilityMethodStatus(capability, "UpdateWorkflowExact")
	return actionJSON(http.StatusOK, map[string]any{
		"workspace":      map[string]any{"id": workspace.Workspace.ID, "name": workspace.Workspace.Name, "resourceVersion": workspace.ResourceVersion},
		"workflows":      items,
		"administration": map[string]any{"create": createAccess, "update": updateAccess},
	})
}

func (p *coordinatorPlugin) applyWorkspaceAdmin(ctx context.Context, workspaceID string, input workspaceAdminActionRequest) (*pluginsdk.PluginActionResponse, error) {
	if input.RequestID == "" {
		return actionError(http.StatusBadRequest, "workspace administration needs a stable request identity"), nil
	}
	method := ""
	switch input.Operation {
	case "workflow.create":
		if strings.TrimSpace(valueOf(input.Name)) == "" || input.ExpectedWorkspaceResourceVersion == "" {
			return actionError(http.StatusBadRequest, "workflow creation needs a name and observed workspace revision"), nil
		}
		method = "CreateWorkflowExact"
	case "workflow.update":
		if input.WorkflowID == "" || input.ExpectedResourceVersion == "" || (input.Name == nil && input.Description == nil && input.Prompt == nil) {
			return actionError(http.StatusBadRequest, "workflow update needs an ID, observed revision, and at least one field"), nil
		}
		method = "UpdateWorkflowExact"
	default:
		return actionError(http.StatusBadRequest, "workspace administration operation is not supported by this plugin"), nil
	}
	exact, capability, result := p.exactCapability(ctx, p.Host(), workspaceID, method)
	if result != nil {
		return result, nil
	}
	manager, ok := pluginsdk.HostWorkspaceAdministration(exact)
	if !ok {
		return actionError(http.StatusNotImplemented, "workspace administration is unavailable"), nil
	}
	requestID, idempotencyKey := defaultIDs(input.RequestID, input.IdempotencyKey)
	command := pluginsdk.WorkspaceAdminCommand{
		RequestID: requestID, WorkspaceID: workspaceID, IdempotencyKey: idempotencyKey,
		ApprovalRevision: capability.ApprovalRevision, ManifestDigest: capability.ManifestDigest,
	}
	switch input.Operation {
	case "workflow.create":
		command.CreateWorkflow = &pluginsdk.WorkspaceWorkflowCreate{
			ExpectedWorkspaceResourceVersion: input.ExpectedWorkspaceResourceVersion,
			Name:                             *input.Name, Description: valueOf(input.Description), Prompt: valueOf(input.Prompt),
		}
	case "workflow.update":
		command.UpdateWorkflow = &pluginsdk.WorkspaceWorkflowUpdate{
			WorkflowID: input.WorkflowID, ExpectedResourceVersion: input.ExpectedResourceVersion,
			Name: input.Name, Description: input.Description, Prompt: input.Prompt,
		}
	}
	response, err := manager.Apply(ctx, command)
	if err != nil {
		return actionError(http.StatusBadGateway, "Host workspace administration request failed"), nil
	}
	if !commandSucceeded(response) {
		return actionJSON(commandHTTPStatus(response.Status), commandEnvelope{Status: string(response.Status), Reason: response.Reason})
	}
	return actionJSON(http.StatusOK, map[string]any{"status": string(response.Status), "request_id": requestID, "idempotency_key": idempotencyKey})
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

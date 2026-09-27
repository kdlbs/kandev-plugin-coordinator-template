package main

import (
	"context"
	"sync"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

type coordinatorPlugin struct {
	pluginsdk.UnimplementedPlugin
	mu       sync.Mutex
	storeMu  sync.Mutex
	store    *policyStore
	storeErr error
}

var _ pluginsdk.Plugin = (*coordinatorPlugin)(nil)
var _ pluginsdk.ActionHandler = (*coordinatorPlugin)(nil)
var _ pluginsdk.AgentToolPlugin = (*coordinatorPlugin)(nil)

func (p *coordinatorPlugin) policyStore() (*policyStore, error) {
	p.storeMu.Lock()
	defer p.storeMu.Unlock()
	if p.store != nil || p.storeErr != nil {
		return p.store, p.storeErr
	}
	p.store, p.storeErr = openPolicyStoreFromEnvironment()
	return p.store, p.storeErr
}

func (p *coordinatorPlugin) HandleWebhook(context.Context, *pluginsdk.WebhookRequest) (*pluginsdk.WebhookResponse, error) {
	return &pluginsdk.WebhookResponse{Status: 404}, nil
}

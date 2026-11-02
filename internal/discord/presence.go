package discord

import (
	"context"
	"sync"
)

// Presence keeps activity production independent from the selected Discord
// transport. IPC is the official local-client path; Gateway remains explicit.
type Presence struct {
	mu       sync.Mutex
	settings Settings
	next     SessionID
	sessions map[SessionID]backendSessions
	gateway  *Client
	ipc      *ipcClient
}

type backendSessions struct {
	gateway SessionID
	ipc     SessionID
}

func New(ctx context.Context) *Presence {
	return newPresence(ctx, defaultDependencies(), defaultIPCDependencies())
}

func newPresence(ctx context.Context, gatewayDeps dependencies, ipcDeps ipcDependencies) *Presence {
	return &Presence{
		sessions: make(map[SessionID]backendSessions),
		gateway:  newClient(ctx, gatewayDeps),
		ipc:      newIPCClient(ctx, ipcDeps),
	}
}

func (p *Presence) Configure(settings Settings) {
	settings = settings.withDefaults()
	p.mu.Lock()
	p.settings = settings
	p.mu.Unlock()
	if settings.Transport == TransportGateway {
		p.ipc.Configure(Settings{})
		p.gateway.Configure(settings)
		return
	}
	p.gateway.Configure(Settings{})
	p.ipc.Configure(settings)
}

func (p *Presence) Begin(snapshot Snapshot) SessionID {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	p.sessions[p.next] = backendSessions{gateway: p.gateway.Begin(snapshot), ipc: p.ipc.Begin(snapshot)}
	return p.next
}

func (p *Presence) Update(id SessionID, snapshot Snapshot) {
	p.mu.Lock()
	sessions, ok := p.sessions[id]
	p.mu.Unlock()
	if !ok {
		return
	}
	p.gateway.Update(sessions.gateway, snapshot)
	p.ipc.Update(sessions.ipc, snapshot)
}

func (p *Presence) End(id SessionID) {
	p.mu.Lock()
	sessions, ok := p.sessions[id]
	delete(p.sessions, id)
	p.mu.Unlock()
	if !ok {
		return
	}
	p.gateway.End(sessions.gateway)
	p.ipc.End(sessions.ipc)
}

func (p *Presence) Status() Status {
	p.mu.Lock()
	transport := p.settings.Transport
	p.mu.Unlock()
	if transport == TransportGateway {
		return p.gateway.Status()
	}
	return p.ipc.Status()
}

func (p *Presence) Close(ctx context.Context) error {
	ipcErr := p.ipc.Close(ctx)
	gatewayErr := p.gateway.Close(ctx)
	if ipcErr != nil {
		return ipcErr
	}
	return gatewayErr
}

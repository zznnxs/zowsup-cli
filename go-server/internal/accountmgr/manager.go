// Package accountmgr runs one goroutine per WhatsApp account and exposes a
// typed registry so the API layer can start / stop accounts and dispatch
// commands without reaching into per-account state directly.
//
// In M0 the manager wires up account lifecycles and the proxy resolution
// but does not yet drive a real WhatsApp connection — actual connection /
// noise handshake / command execution land in M1+. The shape of the
// Runner interface is what later milestones plug into.
package accountmgr

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zznnxs/zowsup-cli/go-server/internal/eventbus"
	"github.com/zznnxs/zowsup-cli/go-server/internal/proxy"
	"github.com/zznnxs/zowsup-cli/go-server/internal/store"
)

// Runner is the per-account workload. M1 replaces NoopRunner with a real
// WhatsApp client implementation.
type Runner interface {
	// Run blocks until ctx is cancelled or a fatal error occurs. It must
	// publish state changes via the provided eventbus.
	Run(ctx context.Context, deps RunDeps) error
}

// RunDeps bundles dependencies passed to each Runner.
type RunDeps struct {
	Account   store.Account
	Proxy     proxy.Config
	ProxyMgr  *proxy.Manager
	Store     *store.Store
	EventBus  *eventbus.Bus
	StartedAt time.Time
}

// Factory builds a fresh Runner for a given account row.
type Factory func(ctx context.Context, acc store.Account) (Runner, error)

// Manager keeps a single live Runner per account ID.
type Manager struct {
	store    *store.Store
	bus      *eventbus.Bus
	proxyMgr *proxy.Manager
	factory  Factory

	mu      sync.Mutex
	running map[int64]*entry
}

type entry struct {
	cancel  context.CancelFunc
	started time.Time
}

// New creates a Manager. factory may be nil — in that case NoopRunner is
// used (which only emits start/stop events).
func New(s *store.Store, bus *eventbus.Bus, pm *proxy.Manager, f Factory) *Manager {
	if f == nil {
		f = func(_ context.Context, _ store.Account) (Runner, error) { return NoopRunner{}, nil }
	}
	return &Manager{
		store:    s,
		bus:      bus,
		proxyMgr: pm,
		factory:  f,
		running:  make(map[int64]*entry),
	}
}

// Start launches a Runner for the given account if it isn't already running.
func (m *Manager) Start(ctx context.Context, accountID int64) error {
	m.mu.Lock()
	if _, ok := m.running[accountID]; ok {
		m.mu.Unlock()
		return errors.New("account already running")
	}
	m.mu.Unlock()

	acc, err := m.store.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("accountmgr: load account %d: %w", accountID, err)
	}
	pCfg, err := m.resolveProxy(ctx, acc.ProxyID)
	if err != nil {
		return fmt.Errorf("accountmgr: resolve proxy: %w", err)
	}

	runner, err := m.factory(ctx, acc)
	if err != nil {
		return fmt.Errorf("accountmgr: build runner: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	startedAt := time.Now()
	m.mu.Lock()
	m.running[accountID] = &entry{cancel: cancel, started: startedAt}
	m.mu.Unlock()

	m.bus.Publish(eventbus.Event{
		Topic:     "account.starting",
		AccountID: accountID,
		Payload:   map[string]any{"phone": acc.Phone},
	})

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.running, accountID)
			m.mu.Unlock()
		}()
		err := runner.Run(runCtx, RunDeps{
			Account:   acc,
			Proxy:     pCfg,
			ProxyMgr:  m.proxyMgr,
			Store:     m.store,
			EventBus:  m.bus,
			StartedAt: startedAt,
		})
		topic := "account.stopped"
		payload := map[string]any{"phone": acc.Phone}
		if err != nil && !errors.Is(err, context.Canceled) {
			topic = "account.error"
			payload["error"] = err.Error()
		}
		m.bus.Publish(eventbus.Event{Topic: topic, AccountID: accountID, Payload: payload})
	}()
	return nil
}

// Stop cancels the running goroutine for accountID. Returns nil if the
// account is not running.
func (m *Manager) Stop(accountID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.running[accountID]
	if !ok {
		return nil
	}
	e.cancel()
	delete(m.running, accountID)
	return nil
}

// IsRunning reports whether a goroutine is currently active for accountID.
func (m *Manager) IsRunning(accountID int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.running[accountID]
	return ok
}

// RunningIDs returns the set of account IDs currently active.
func (m *Manager) RunningIDs() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int64, 0, len(m.running))
	for id := range m.running {
		out = append(out, id)
	}
	return out
}

// StopAll cancels every running account goroutine.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.running {
		e.cancel()
		delete(m.running, id)
	}
}

// resolveProxy reads the proxy row referenced by an account (if any) and
// builds the corresponding proxy.Config.
func (m *Manager) resolveProxy(ctx context.Context, proxyID *int64) (proxy.Config, error) {
	if proxyID == nil {
		return proxy.Config{}, nil
	}
	rows, err := m.store.ListProxies(ctx)
	if err != nil {
		return proxy.Config{}, err
	}
	for _, p := range rows {
		if p.ID != *proxyID {
			continue
		}
		if p.Template != "" {
			return proxy.ParseTemplate(p.Template)
		}
		return proxy.Config{
			Scheme:   p.Scheme,
			Host:     p.Host,
			Port:     p.Port,
			Username: p.Username,
			Password: p.Password,
		}, nil
	}
	return proxy.Config{}, fmt.Errorf("accountmgr: proxy %d not found", *proxyID)
}

// NoopRunner is the default Runner used until M1 lands a real one. It
// blocks until ctx is cancelled and publishes a single heartbeat per
// second so the dashboard has something to draw.
type NoopRunner struct{}

// Run implements Runner.
func (NoopRunner) Run(ctx context.Context, deps RunDeps) error {
	deps.EventBus.Publish(eventbus.Event{
		Topic:     "account.running",
		AccountID: deps.Account.ID,
		Payload: map[string]any{
			"phone":   deps.Account.Phone,
			"proxy":   deps.Proxy.String(),
			"started": deps.StartedAt.Format(time.RFC3339),
		},
	})
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			deps.EventBus.Publish(eventbus.Event{
				Topic:     "account.heartbeat",
				AccountID: deps.Account.ID,
				Payload:   map[string]any{"at": t.Unix()},
			})
		}
	}
}

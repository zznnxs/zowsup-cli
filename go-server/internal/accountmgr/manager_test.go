package accountmgr

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zznnxs/zowsup-cli/go-server/internal/db"
	"github.com/zznnxs/zowsup-cli/go-server/internal/eventbus"
	"github.com/zznnxs/zowsup-cli/go-server/internal/proxy"
	"github.com/zznnxs/zowsup-cli/go-server/internal/store"
)

func newTestEnv(t *testing.T) (*store.Store, *eventbus.Bus, *proxy.Manager) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return store.New(d), eventbus.New(8), proxy.NewManager()
}

type recRunner struct{ ran chan struct{} }

func (r *recRunner) Run(ctx context.Context, _ RunDeps) error {
	close(r.ran)
	<-ctx.Done()
	return ctx.Err()
}

func TestStartStopSingleAccount(t *testing.T) {
	s, bus, pm := newTestEnv(t)
	id, err := s.CreateAccount(context.Background(), store.Account{Phone: "p", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recRunner{ran: make(chan struct{})}
	m := New(s, bus, pm, func(_ context.Context, _ store.Account) (Runner, error) { return rec, nil })

	if err := m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}

	select {
	case <-rec.ran:
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}

	if !m.IsRunning(id) {
		t.Fatal("expected IsRunning to be true")
	}
	if err := m.Start(context.Background(), id); err == nil {
		t.Fatal("double start should fail")
	}
	if err := m.Stop(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for m.IsRunning(id) {
		select {
		case <-deadline:
			t.Fatal("account did not stop in time")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestStopAll(t *testing.T) {
	s, bus, pm := newTestEnv(t)
	a1, _ := s.CreateAccount(context.Background(), store.Account{Phone: "a", Platform: "android"})
	a2, _ := s.CreateAccount(context.Background(), store.Account{Phone: "b", Platform: "android"})
	m := New(s, bus, pm, nil) // NoopRunner
	if err := m.Start(context.Background(), a1); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background(), a2); err != nil {
		t.Fatal(err)
	}
	m.StopAll()
	deadline := time.After(time.Second)
	for len(m.RunningIDs()) > 0 {
		select {
		case <-deadline:
			t.Fatalf("StopAll left runners: %v", m.RunningIDs())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestProxyAttachedToRunner(t *testing.T) {
	s, bus, pm := newTestEnv(t)
	pid, _ := s.CreateProxy(context.Background(), store.Proxy{
		Name: "px", Scheme: "socks5", Host: "1.2.3.4", Port: 1080,
		Username: "u", Password: "p",
	})
	id, _ := s.CreateAccount(context.Background(), store.Account{Phone: "p", Platform: "android", ProxyID: &pid})

	gotProxy := make(chan string, 1)
	f := func(_ context.Context, _ store.Account) (Runner, error) {
		return runnerFn(func(ctx context.Context, deps RunDeps) error {
			gotProxy <- deps.Proxy.String()
			<-ctx.Done()
			return ctx.Err()
		}), nil
	}
	m := New(s, bus, pm, f)
	if err := m.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(id)

	select {
	case p := <-gotProxy:
		want := "1.2.3.4:1080:u:p"
		if p != want {
			t.Fatalf("got proxy %q want %q", p, want)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not start")
	}
}

type runnerFn func(ctx context.Context, deps RunDeps) error

func (f runnerFn) Run(ctx context.Context, deps RunDeps) error { return f(ctx, deps) }

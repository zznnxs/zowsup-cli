package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zznnxs/zowsup-cli/go-server/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		d.Close()
		os.Remove(path)
	})
	return New(d)
}

func TestProxiesCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.CreateProxy(ctx, Proxy{
		Name: "hk-1", Scheme: "socks5", Host: "1.2.3.4", Port: 1080,
		Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id <= 0 {
		t.Fatal("expected positive id")
	}
	got, err := s.ListProxies(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "hk-1" || got[0].Username != "u" {
		t.Fatalf("unexpected: %+v", got)
	}
	if err := s.DeleteProxy(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListProxies(ctx)
	if len(got) != 0 {
		t.Fatal("delete did not remove row")
	}
}

func TestAccountsCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	proxyID, err := s.CreateProxy(ctx, Proxy{Name: "p", Scheme: "http", Host: "h", Port: 80})
	if err != nil {
		t.Fatal(err)
	}

	id, err := s.CreateAccount(ctx, Account{
		Phone:    "628111",
		Platform: "android",
		ProxyID:  &proxyID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Phone != "628111" || got.Status != "created" || got.ProxyID == nil || *got.ProxyID != proxyID {
		t.Fatalf("unexpected: %+v", got)
	}

	if err := s.UpdateAccountStatus(ctx, id, "connected", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAccount(ctx, id)
	if got.Status != "connected" {
		t.Fatalf("status not updated: %s", got.Status)
	}

	if err := s.SetAccountProxy(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAccount(ctx, id)
	if got.ProxyID != nil {
		t.Fatal("proxy not cleared")
	}

	if err := s.DeleteAccount(ctx, id); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetAccount(ctx, id)
	if err == nil {
		t.Fatal("expected not-found after delete")
	}
}

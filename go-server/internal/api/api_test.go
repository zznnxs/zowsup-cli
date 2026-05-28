package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/zznnxs/zowsup-cli/go-server/internal/accountmgr"
	"github.com/zznnxs/zowsup-cli/go-server/internal/db"
	"github.com/zznnxs/zowsup-cli/go-server/internal/eventbus"
	"github.com/zznnxs/zowsup-cli/go-server/internal/proxy"
	"github.com/zznnxs/zowsup-cli/go-server/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, Deps) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := store.New(d)
	bus := eventbus.New(8)
	pm := proxy.NewManager()
	mgr := accountmgr.New(s, bus, pm, nil)
	deps := Deps{Store: s, Manager: mgr, Bus: bus, ProxyMgr: pm}
	srv := httptest.NewServer(Router(deps))
	t.Cleanup(srv.Close)
	t.Cleanup(mgr.StopAll)
	return srv, deps
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestProxyAndAccountFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	c := srv.Client()

	// create proxy
	resp, err := c.Post(srv.URL+"/api/proxies", "application/json", bytes.NewBufferString(
		`{"name":"hk","scheme":"socks5","host":"1.2.3.4","port":1080,"username":"u","password":"p"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create proxy status: %d", resp.StatusCode)
	}
	var px struct{ ID int64 }
	if err := json.NewDecoder(resp.Body).Decode(&px); err != nil {
		t.Fatal(err)
	}
	if px.ID == 0 {
		t.Fatal("no id returned")
	}

	// create account with proxy
	body, _ := json.Marshal(map[string]any{"phone": "628111", "proxy_id": px.ID})
	resp, err = c.Post(srv.URL+"/api/accounts", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create account status: %d", resp.StatusCode)
	}
	var ac struct{ ID int64 }
	_ = json.NewDecoder(resp.Body).Decode(&ac)

	// list accounts
	resp, err = c.Get(srv.URL + "/api/accounts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&rows)
	if len(rows) != 1 || rows[0]["phone"] != "628111" || rows[0]["running"] != false {
		t.Fatalf("unexpected list: %v", rows)
	}
}

func TestStartStopAccountAndEvents(t *testing.T) {
	srv, deps := newTestServer(t)
	c := srv.Client()

	// Bypass full HTTP create — go straight to the store.
	id, err := deps.Store.CreateAccount(context.Background(), store.Account{Phone: "p", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}

	// Open ws first so we don't miss the start event.
	wsURL := "ws" + srv.URL[len("http"):] + "/api/events"
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wsCancel()
	conn, _, err := websocket.Dial(wsCtx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	// start
	resp, err := c.Post(srv.URL+"/api/accounts/"+itoa(id)+"/start", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status: %d", resp.StatusCode)
	}

	// expect at least one event before timeout.
	gotStart := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(wsCtx, 500*time.Millisecond)
		var ev map[string]any
		err := wsjson.Read(readCtx, conn, &ev)
		cancel()
		if err == nil {
			if topic, _ := ev["topic"].(string); topic == "account.starting" || topic == "account.running" {
				gotStart = true
				break
			}
		}
	}
	if !gotStart {
		t.Fatal("did not receive starting/running event")
	}

	// stop
	resp, err = c.Post(srv.URL+"/api/accounts/"+itoa(id)+"/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

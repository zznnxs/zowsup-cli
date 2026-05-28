// Package api exposes the HTTP REST + WebSocket endpoints. The M0 surface
// covers account/proxy CRUD and a `/api/events` websocket that pipes every
// eventbus event to the browser; per-command endpoints arrive in M2+.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/zznnxs/zowsup-cli/go-server/internal/accountmgr"
	"github.com/zznnxs/zowsup-cli/go-server/internal/eventbus"
	"github.com/zznnxs/zowsup-cli/go-server/internal/proxy"
	"github.com/zznnxs/zowsup-cli/go-server/internal/store"
)

// Deps bundles dependencies the router needs.
type Deps struct {
	Store    *store.Store
	Manager  *accountmgr.Manager
	Bus      *eventbus.Bus
	ProxyMgr *proxy.Manager
}

// Router returns a chi router with all routes wired up.
func Router(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// /api/events hijacks the connection for a websocket; chi's Timeout
	// middleware wraps the response writer and tries to WriteHeader after
	// the deadline, which logs spurious errors and racks up data races
	// against the hijacked conn. Keep Timeout scoped to REST handlers only.
	r.Get("/api/events", d.events)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(60 * time.Second))
		r.Get("/api/healthz", healthz)
		r.Route("/api/proxies", func(r chi.Router) {
			r.Get("/", d.listProxies)
			r.Post("/", d.createProxy)
			r.Delete("/{id}", d.deleteProxy)
		})
		r.Route("/api/accounts", func(r chi.Router) {
			r.Get("/", d.listAccounts)
			r.Post("/", d.createAccount)
			r.Get("/{id}", d.getAccount)
			r.Delete("/{id}", d.deleteAccount)
			r.Patch("/{id}/proxy", d.setAccountProxy)
			r.Post("/{id}/start", d.startAccount)
			r.Post("/{id}/stop", d.stopAccount)
		})
	})
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().Unix()})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// proxies
// ---------------------------------------------------------------------------

func (d Deps) listProxies(w http.ResponseWriter, r *http.Request) {
	rows, err := d.Store.ListProxies(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

type createProxyReq struct {
	Name     string `json:"name"`
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Template string `json:"template,omitempty"`
}

func (d Deps) createProxy(w http.ResponseWriter, r *http.Request) {
	var req createProxyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Template != "" {
		cfg, err := proxy.ParseTemplate(req.Template)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		req.Scheme = cfg.Scheme
		req.Host = cfg.Host
		req.Port = cfg.Port
		req.Username = cfg.Username
		req.Password = cfg.Password
	}
	if req.Name == "" || req.Scheme == "" || req.Host == "" || req.Port == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("name, scheme, host, port are required"))
		return
	}
	id, err := d.Store.CreateProxy(r.Context(), store.Proxy{
		Name: req.Name, Scheme: req.Scheme, Host: req.Host, Port: req.Port,
		Username: req.Username, Password: req.Password, Template: req.Template,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (d Deps) deleteProxy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Store.DeleteProxy(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// accounts
// ---------------------------------------------------------------------------

func (d Deps) listAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := d.Store.ListAccounts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	running := map[int64]bool{}
	for _, id := range d.Manager.RunningIDs() {
		running[id] = true
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, accountToJSON(a, running[a.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

type createAccountReq struct {
	Phone    string `json:"phone"`
	Platform string `json:"platform,omitempty"`
	ProxyID  *int64 `json:"proxy_id,omitempty"`
}

func (d Deps) createAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Phone == "" {
		writeErr(w, http.StatusBadRequest, errors.New("phone is required"))
		return
	}
	id, err := d.Store.CreateAccount(r.Context(), store.Account{
		Phone: req.Phone, Platform: req.Platform, ProxyID: req.ProxyID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (d Deps) getAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a, err := d.Store.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, accountToJSON(a, d.Manager.IsRunning(a.ID)))
}

func (d Deps) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_ = d.Manager.Stop(id)
	if err := d.Store.DeleteAccount(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setProxyReq struct {
	ProxyID *int64 `json:"proxy_id"`
}

func (d Deps) setAccountProxy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req setProxyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Store.SetAccountProxy(r.Context(), id, req.ProxyID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) startAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.Manager.Start(r.Context(), id); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (d Deps) stopAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_ = d.Manager.Stop(id)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// events websocket
// ---------------------------------------------------------------------------

func (d Deps) events(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // dev: allow vite at :5173 → :8080
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "bye")

	sub := d.Bus.Subscribe()
	defer sub.Cancel()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Reader (drops pongs / client messages).
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			payload := map[string]any{
				"topic":      ev.Topic,
				"account_id": ev.AccountID,
				"payload":    ev.Payload,
				"at":         ev.At.Unix(),
			}
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(wctx, c, payload)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func accountToJSON(a store.Account, running bool) map[string]any {
	out := map[string]any{
		"id":         a.ID,
		"phone":      a.Phone,
		"device_id":  a.DeviceID,
		"push_name":  a.PushName,
		"platform":   a.Platform,
		"status":     a.Status,
		"running":    running,
		"created_at": a.CreatedAt.Unix(),
		"updated_at": a.UpdatedAt.Unix(),
	}
	if a.ProxyID != nil {
		out["proxy_id"] = *a.ProxyID
	}
	if a.LastSeenAt != nil {
		out["last_seen_at"] = a.LastSeenAt.Unix()
	}
	return out
}

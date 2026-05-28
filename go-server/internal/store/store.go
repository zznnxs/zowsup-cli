// Package store wraps the modernc.org/sqlite database with typed CRUD
// helpers for accounts, proxies, contacts, chats, messages, etc.
//
// Methods take a context.Context and return Go-typed structs; the API
// layer and account manager talk to the database exclusively through this
// package so that schema changes are localized.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Store is the typed handle returned by New.
type Store struct {
	db *sql.DB
}

// New wraps an already-open *sql.DB. The caller is responsible for closing
// the underlying handle.
func New(db *sql.DB) *Store { return &Store{db: db} }

// DB returns the underlying *sql.DB. Use sparingly — prefer typed helpers.
func (s *Store) DB() *sql.DB { return s.db }

// ErrNotFound is returned when a unique lookup misses.
var ErrNotFound = errors.New("store: not found")

// ---------------------------------------------------------------------------
// Proxies
// ---------------------------------------------------------------------------

// Proxy mirrors a row from the proxies table.
type Proxy struct {
	ID        int64
	Name      string
	Scheme    string
	Host      string
	Port      int
	Username  string
	Password  string
	Template  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListProxies returns every proxy ordered by ID.
func (s *Store) ListProxies(ctx context.Context) ([]Proxy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,scheme,host,port,
		COALESCE(username,''),COALESCE(password,''),COALESCE(template,''),
		created_at,updated_at FROM proxies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proxy
	for rows.Next() {
		var p Proxy
		var c, u int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Scheme, &p.Host, &p.Port,
			&p.Username, &p.Password, &p.Template, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt = time.Unix(c, 0)
		p.UpdatedAt = time.Unix(u, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateProxy inserts p (ignoring ID/CreatedAt/UpdatedAt) and returns the
// inserted ID.
func (s *Store) CreateProxy(ctx context.Context, p Proxy) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO proxies(name,scheme,host,port,username,password,template,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		p.Name, p.Scheme, p.Host, p.Port,
		nullableString(p.Username), nullableString(p.Password),
		nullableString(p.Template), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteProxy removes a proxy row.
func (s *Store) DeleteProxy(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxies WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

// Account mirrors a row from the accounts table.
type Account struct {
	ID         int64
	Phone      string
	DeviceID   int
	PushName   string
	Platform   string
	Status     string
	ProxyID    *int64
	LastSeenAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ListAccounts returns all accounts ordered by phone.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,phone,device_id,
		COALESCE(push_name,''),platform,status,proxy_id,last_seen_at,
		created_at,updated_at FROM accounts ORDER BY phone`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount fetches a single account by id.
func (s *Store) GetAccount(ctx context.Context, id int64) (Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,phone,device_id,
		COALESCE(push_name,''),platform,status,proxy_id,last_seen_at,
		created_at,updated_at FROM accounts WHERE id = ?`, id)
	a, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// CreateAccount inserts a new account. Status defaults to "created".
func (s *Store) CreateAccount(ctx context.Context, a Account) (int64, error) {
	if a.Phone == "" {
		return 0, errors.New("store: account phone is required")
	}
	if a.Platform == "" {
		a.Platform = "android"
	}
	if a.Status == "" {
		a.Status = "created"
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts(phone,device_id,push_name,platform,status,proxy_id,
		 last_seen_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.Phone, a.DeviceID, nullableString(a.PushName), a.Platform, a.Status,
		nullableInt(a.ProxyID), nullableUnix(a.LastSeenAt), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAccountStatus sets the status / last_seen_at columns and bumps updated_at.
func (s *Store) UpdateAccountStatus(ctx context.Context, id int64, status string, seenAt *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE accounts SET status = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`,
		status, nullableUnix(seenAt), time.Now().Unix(), id)
	return err
}

// SetAccountProxy assigns a proxy (or nil to clear).
func (s *Store) SetAccountProxy(ctx context.Context, id int64, proxyID *int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE accounts SET proxy_id = ?, updated_at = ? WHERE id = ?`,
		nullableInt(proxyID), time.Now().Unix(), id)
	return err
}

// DeleteAccount removes the account and (via ON DELETE CASCADE) every
// related row.
func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type rowLike interface {
	Scan(dest ...any) error
}

func scanAccount(r rowLike) (Account, error) {
	var (
		a               Account
		proxyID         sql.NullInt64
		lastSeen        sql.NullInt64
		createdAt, upAt int64
	)
	if err := r.Scan(&a.ID, &a.Phone, &a.DeviceID, &a.PushName, &a.Platform,
		&a.Status, &proxyID, &lastSeen, &createdAt, &upAt); err != nil {
		return Account{}, err
	}
	if proxyID.Valid {
		v := proxyID.Int64
		a.ProxyID = &v
	}
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0)
		a.LastSeenAt = &t
	}
	a.CreatedAt = time.Unix(createdAt, 0)
	a.UpdatedAt = time.Unix(upAt, 0)
	return a, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

func nullableUnix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// MustExec is a convenience for migrations / setup paths where any error
// is fatal. Not used in normal request handling.
func (s *Store) MustExec(ctx context.Context, query string, args ...any) {
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		panic(fmt.Errorf("store: must exec %q: %w", query, err))
	}
}

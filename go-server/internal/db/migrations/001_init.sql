-- migrations/001_init.sql
-- Schema for the modernc.org/sqlite store. Single file, WAL mode, applied
-- once at startup. Keep statements idempotent (CREATE ... IF NOT EXISTS).

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  INTEGER NOT NULL
);

-- ---------------------------------------------------------------------------
-- Accounts and proxies
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS proxies (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    scheme          TEXT NOT NULL CHECK (scheme IN ('socks5','http','https')),
    host            TEXT NOT NULL,
    port            INTEGER NOT NULL,
    username        TEXT,
    password        TEXT,
    -- raw template string that may contain {session_id} / {location} placeholders
    template        TEXT,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    phone           TEXT NOT NULL UNIQUE,
    device_id       INTEGER NOT NULL DEFAULT 0,
    push_name       TEXT,
    platform        TEXT NOT NULL DEFAULT 'android',
    status          TEXT NOT NULL DEFAULT 'created',
    -- pairing / registration / connected / disconnected / banned
    proxy_id        INTEGER REFERENCES proxies(id) ON DELETE SET NULL,
    last_seen_at    INTEGER,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status);

-- ---------------------------------------------------------------------------
-- Signal / Axolotl identity, sessions, prekeys, sender keys (per account)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS identities (
    account_id              INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    identity_key_priv       BLOB NOT NULL,
    identity_key_pub        BLOB NOT NULL,
    registration_id         INTEGER NOT NULL,
    noise_key_priv          BLOB NOT NULL,
    noise_key_pub           BLOB NOT NULL,
    signed_prekey_id        INTEGER NOT NULL DEFAULT 1,
    signed_prekey_priv      BLOB NOT NULL,
    signed_prekey_pub       BLOB NOT NULL,
    signed_prekey_signature BLOB NOT NULL,
    adv_secret              BLOB NOT NULL,
    -- companion-side identity proof; populated after successful pair
    account_signature       BLOB,
    device_signature        BLOB,
    server_token            BLOB,
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS prekeys (
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key_id          INTEGER NOT NULL,
    priv_key        BLOB NOT NULL,
    pub_key         BLOB NOT NULL,
    uploaded        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, key_id)
);

CREATE INDEX IF NOT EXISTS idx_prekeys_uploaded
    ON prekeys(account_id, uploaded);

CREATE TABLE IF NOT EXISTS sessions (
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    address         TEXT NOT NULL,        -- "<jid>:<device>"
    record          BLOB NOT NULL,        -- serialized SessionRecord
    PRIMARY KEY (account_id, address)
);

CREATE TABLE IF NOT EXISTS sender_keys (
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_id        TEXT NOT NULL,
    sender_id       TEXT NOT NULL,
    record          BLOB NOT NULL,
    PRIMARY KEY (account_id, group_id, sender_id)
);

-- ---------------------------------------------------------------------------
-- Application-level data
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS contacts (
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    jid             TEXT NOT NULL,
    push_name       TEXT,
    business_name   TEXT,
    notify_name     TEXT,
    profile_pic_url TEXT,
    trusted         INTEGER NOT NULL DEFAULT 0,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (account_id, jid)
);

CREATE TABLE IF NOT EXISTS chats (
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    jid             TEXT NOT NULL,
    name            TEXT,
    is_group        INTEGER NOT NULL DEFAULT 0,
    unread          INTEGER NOT NULL DEFAULT 0,
    last_msg_at     INTEGER,
    last_msg_id     TEXT,
    PRIMARY KEY (account_id, jid)
);

CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    chat_jid        TEXT NOT NULL,
    msg_id          TEXT NOT NULL,
    sender_jid      TEXT,
    direction       TEXT NOT NULL CHECK (direction IN ('in','out')),
    type            TEXT NOT NULL,        -- text/image/video/audio/document/sticker/ad/...
    body            TEXT,
    media_url       TEXT,
    media_mime      TEXT,
    media_sha256    BLOB,
    quoted_msg_id   TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    ts              INTEGER NOT NULL,
    raw_payload     BLOB,
    UNIQUE (account_id, chat_jid, msg_id)
);

CREATE INDEX IF NOT EXISTS idx_messages_chat_ts
    ON messages(account_id, chat_jid, ts DESC);

-- ---------------------------------------------------------------------------
-- Audit: pairing attempts, agent jobs, etc. (used in later milestones)
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS pair_attempts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id      INTEGER REFERENCES accounts(id) ON DELETE SET NULL,
    phone           TEXT,
    method          TEXT NOT NULL CHECK (method IN ('qr','code')),
    code            TEXT,                 -- 8-char pair code, only for method=code
    ref             TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    error           TEXT,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

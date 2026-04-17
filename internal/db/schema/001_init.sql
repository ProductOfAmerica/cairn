-- cairn Ship 1 schema.
-- Timestamps are integer milliseconds since epoch.
-- IDs are ULIDs except op_id (caller-supplied) and events.id (autoincrement).
-- Cuts applied (Ship 1):
--   - No `sensitivity` column anywhere. Reintroduce when producer polymorphism arrives.
--   - Single `producer_hash` instead of split producer_user_hash + producer_vendor_hash.
--   - Binary staleness only: a verdict is fresh if gate_def_hash matches AND inputs_hash matches
--     AND status='pass'. Otherwise stale. No soft/hard distinction.
--   - No memory_entries / memory_fts yet (Ship 2).

CREATE TABLE requirements (
    id                TEXT PRIMARY KEY,
    spec_path         TEXT NOT NULL,
    spec_hash         TEXT NOT NULL,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);

CREATE TABLE gates (
    id                TEXT PRIMARY KEY,
    requirement_id    TEXT NOT NULL REFERENCES requirements(id),
    kind              TEXT NOT NULL,              -- test|property|rubric|human|custom
    definition_json   TEXT NOT NULL,
    gate_def_hash     TEXT NOT NULL,
    producer_kind     TEXT NOT NULL,              -- executable|human|agent|pipeline
    producer_config   TEXT NOT NULL
);

CREATE INDEX idx_gates_requirement ON gates(requirement_id);

CREATE TABLE tasks (
    id                  TEXT PRIMARY KEY,
    requirement_id      TEXT NOT NULL REFERENCES requirements(id),
    spec_path           TEXT NOT NULL,
    spec_hash           TEXT NOT NULL,
    depends_on_json     TEXT NOT NULL DEFAULT '[]',
    required_gates_json TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL,            -- open|claimed|in_progress|gate_pending|done|failed|stale
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);

CREATE INDEX idx_tasks_status ON tasks(status);

CREATE TABLE claims (
    id                TEXT PRIMARY KEY,
    task_id           TEXT NOT NULL REFERENCES tasks(id),
    agent_id          TEXT NOT NULL,
    acquired_at       INTEGER NOT NULL,
    expires_at        INTEGER NOT NULL,
    released_at       INTEGER,
    op_id             TEXT NOT NULL UNIQUE
);

CREATE INDEX idx_claims_task_live ON claims(task_id, released_at, expires_at);

CREATE TABLE runs (
    id                TEXT PRIMARY KEY,
    task_id           TEXT NOT NULL REFERENCES tasks(id),
    claim_id          TEXT NOT NULL REFERENCES claims(id),
    started_at        INTEGER NOT NULL,
    ended_at          INTEGER,
    outcome           TEXT                        -- done|failed|orphaned|NULL
);

CREATE INDEX idx_runs_task ON runs(task_id);
CREATE INDEX idx_runs_claim ON runs(claim_id);

CREATE TABLE evidence (
    id                TEXT PRIMARY KEY,
    sha256            TEXT NOT NULL UNIQUE,
    uri               TEXT NOT NULL,
    bytes             INTEGER NOT NULL,
    content_type      TEXT NOT NULL,
    created_at        INTEGER NOT NULL
);

-- Verdicts are append-only. Staleness is derived, never stored.
CREATE TABLE verdicts (
    id                TEXT PRIMARY KEY,
    run_id            TEXT NOT NULL REFERENCES runs(id),
    gate_id           TEXT NOT NULL REFERENCES gates(id),
    status            TEXT NOT NULL,              -- pass|fail|inconclusive
    score_json        TEXT,
    producer_hash     TEXT NOT NULL,
    gate_def_hash     TEXT NOT NULL,
    inputs_hash       TEXT NOT NULL,
    evidence_id       TEXT REFERENCES evidence(id),
    bound_at          INTEGER NOT NULL,
    sequence          INTEGER NOT NULL
);

CREATE INDEX idx_verdicts_latest ON verdicts(gate_id, bound_at DESC, sequence DESC);

CREATE TABLE events (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    at                INTEGER NOT NULL,
    kind              TEXT NOT NULL,
    entity_kind       TEXT NOT NULL,
    entity_id         TEXT NOT NULL,
    payload_json      TEXT NOT NULL,
    op_id             TEXT
);

CREATE INDEX idx_events_entity ON events(entity_kind, entity_id);
CREATE INDEX idx_events_at ON events(at);

-- Idempotency log. Callers pass --op-id with every mutation; replays return cached result.
CREATE TABLE op_log (
    op_id             TEXT PRIMARY KEY,
    kind              TEXT NOT NULL,
    first_seen_at     INTEGER NOT NULL,
    result_json       TEXT NOT NULL
);

-- Migration tracking.
CREATE TABLE IF NOT EXISTS schema_migrations (
    version           INTEGER PRIMARY KEY,
    applied_at        INTEGER NOT NULL
);

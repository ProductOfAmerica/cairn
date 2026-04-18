-- cairn Ship 2 schema.
-- Adds memory (append-only with FTS5) + evidence invalidation plumbing.
-- Amends Ship 1 gap: makes evidence rows append-only at the schema level,
-- matching the discipline already applied to verdicts.

-- Part A: memory

CREATE TABLE memory_entries (
    id          TEXT PRIMARY KEY,
    at          INTEGER NOT NULL,
    kind        TEXT NOT NULL,                         -- decision|rationale|outcome|failure (CLI-validated)
    entity_kind TEXT,                                  -- CLI-validated enum when non-null
    entity_id   TEXT,                                  -- free text; no FK
    body        TEXT NOT NULL,
    tags_json   TEXT NOT NULL DEFAULT '[]',            -- structured output
    tags_text   TEXT NOT NULL DEFAULT '',              -- space-joined; FTS5-indexed
    CHECK ((entity_kind IS NULL) = (entity_id IS NULL))
);

CREATE INDEX idx_memory_at     ON memory_entries(at DESC);
CREATE INDEX idx_memory_kind   ON memory_entries(kind, at DESC);
CREATE INDEX idx_memory_entity ON memory_entries(entity_kind, entity_id);

CREATE VIRTUAL TABLE memory_fts USING fts5(body, tags);

CREATE TRIGGER memory_fts_ai AFTER INSERT ON memory_entries BEGIN
    INSERT INTO memory_fts(rowid, body, tags)
    VALUES (new.rowid, new.body, new.tags_text);
END;

CREATE TRIGGER memory_no_delete BEFORE DELETE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

CREATE TRIGGER memory_no_update BEFORE UPDATE ON memory_entries BEGIN
    SELECT RAISE(ABORT, 'memory is append-only');
END;

-- Part B: evidence invalidation signal + schema-level append-only enforcement.

ALTER TABLE evidence ADD COLUMN invalidated_at INTEGER;

CREATE TRIGGER evidence_only_invalidated_at_updatable
BEFORE UPDATE ON evidence
FOR EACH ROW
WHEN new.id           IS NOT old.id
  OR new.sha256       IS NOT old.sha256
  OR new.uri          IS NOT old.uri
  OR new.bytes        IS NOT old.bytes
  OR new.content_type IS NOT old.content_type
  OR new.created_at   IS NOT old.created_at
BEGIN
    SELECT RAISE(ABORT, 'evidence is append-only except invalidated_at');
END;

CREATE TRIGGER evidence_no_delete BEFORE DELETE ON evidence BEGIN
    SELECT RAISE(ABORT, 'evidence rows cannot be deleted');
END;

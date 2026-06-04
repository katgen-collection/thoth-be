-- Thothai initial schema — all 5 tables (Phase 0).
-- pgcrypto provides gen_random_uuid().

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ─── search_jobs ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS search_jobs (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          VARCHAR(255) NOT NULL,
    query            TEXT NOT NULL,
    -- pending | extracting | fetching_jobs | ai_filtering | completed | failed
    status           VARCHAR(50) NOT NULL DEFAULT 'pending',
    progress         INT NOT NULL DEFAULT 0,
    extracted_params JSONB,
    result           JSONB,
    error            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_search_jobs_user_id ON search_jobs (user_id);

-- ─── conversations ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS conversations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    VARCHAR(255) NOT NULL,
    title      VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_conversations_user_id ON conversations (user_id);

-- ─── messages ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            VARCHAR(20) NOT NULL, -- user | assistant | tool
    content         TEXT,
    tool_name       VARCHAR(100),
    tool_args       JSONB,
    tool_result     JSONB,
    search_job_id   UUID REFERENCES search_jobs (id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_messages_conversation_id ON messages (conversation_id);

-- ─── cvs ─────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cvs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        VARCHAR(255) NOT NULL,
    filename       VARCHAR(255) NOT NULL,
    storage_path   VARCHAR(500) NOT NULL,
    file_size      INT,
    extracted_text TEXT,
    parsed_data    JSONB,
    is_default     BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_cvs_user_id ON cvs (user_id);

-- ─── saved_jobs ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS saved_jobs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     VARCHAR(255) NOT NULL,
    title       VARCHAR(255) NOT NULL,
    company     VARCHAR(255),
    location    VARCHAR(255),
    description TEXT,
    apply_link  TEXT,
    source      JSONB,
    -- saved | applied | interview | offer | rejected
    status      VARCHAR(50) NOT NULL DEFAULT 'saved',
    notes       TEXT,
    applied_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ix_saved_jobs_user_id ON saved_jobs (user_id);

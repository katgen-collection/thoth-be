-- Phase 3 — CV upload security columns.
-- Rename storage_path -> storage_key (opaque object key) and add content
-- metadata + the upload/parse status state machine.

ALTER TABLE cvs RENAME COLUMN storage_path TO storage_key;

ALTER TABLE cvs ADD COLUMN IF NOT EXISTS mime_type VARCHAR(100);
ALTER TABLE cvs ADD COLUMN IF NOT EXISTS sha256 CHAR(64);

-- uploaded | scanning | parsing | ready | rejected | failed
ALTER TABLE cvs ADD COLUMN IF NOT EXISTS status VARCHAR(50) NOT NULL DEFAULT 'uploaded';

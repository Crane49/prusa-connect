ALTER TABLE note ADD COLUMN written_at TEXT NOT NULL DEFAULT '';
CREATE INDEX note_written_at ON note (written_at);

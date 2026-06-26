-- 012_checkin_media_sqlite.sql — binary attachments for QR self-check-in.
--
-- Mirrored in ensureSchemaSQL (internal/db/admin.go), which is what the running
-- Go app self-provisions at startup. This file is the canonical record (same
-- convention as migrations 001–011).
--
-- checkin_media holds the heavy image blobs captured on the public check-in page
-- — the client's selfie and (when they draw rather than type) their signature —
-- kept OUT of the wide `checkins` row so the approval queue's SELECT stays light.
-- Each blob's sha256 is sealed INTO the checkin's hash chain (selfie_path /
-- signature_data carry "sha256:…"), so swapping a stored image is detectable even
-- though the bytes themselves live here. base64 keeps the single-file SQLite
-- deploy story (no filesystem path to ship to ptr1).

CREATE TABLE IF NOT EXISTS checkin_media (
    media_id   INTEGER PRIMARY KEY AUTOINCREMENT,
    checkin_id INTEGER NOT NULL,
    kind       TEXT NOT NULL,        -- 'selfie' | 'signature'
    mime       TEXT NOT NULL,        -- 'image/jpeg'
    sha256     TEXT NOT NULL,        -- digest sealed into the checkin row's hash
    image_b64  TEXT NOT NULL,        -- base64-encoded image bytes
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_checkin_media ON checkin_media(checkin_id, kind);

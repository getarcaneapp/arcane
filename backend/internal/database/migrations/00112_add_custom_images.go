package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v112SQLiteUp = `
CREATE TABLE custom_images (
    id TEXT PRIMARY KEY,
    slot TEXT NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    data BLOB NOT NULL,
    mime_type TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME
);
CREATE UNIQUE INDEX idx_custom_images_slot_size ON custom_images (slot, size);
`
	v112PostgresUp = `
CREATE TABLE IF NOT EXISTS custom_images (
    id text PRIMARY KEY,
    slot text NOT NULL,
    size integer NOT NULL DEFAULT 0,
    data bytea NOT NULL,
    mime_type text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_custom_images_slot_size ON custom_images (slot, size);
`
	v112SQLiteDown = `
DROP TABLE custom_images;
DELETE FROM settings WHERE key IN ('customLogoHash', 'customLogoFullHash', 'customFaviconHash', 'customPwaIconHash');
`
	v112PostgresDown = `
DROP TABLE IF EXISTS custom_images;
DELETE FROM settings WHERE key IN ('customLogoHash', 'customLogoFullHash', 'customFaviconHash', 'customPwaIconHash');
`
)

func init() {
	goose.AddMigrationContext(run(v112SQLiteUp, v112PostgresUp), run(v112SQLiteDown, v112PostgresDown))
}

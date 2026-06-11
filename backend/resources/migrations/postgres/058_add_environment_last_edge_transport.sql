-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS last_edge_transport TEXT;

-- +goose Down
ALTER TABLE environments DROP COLUMN IF EXISTS last_edge_transport;

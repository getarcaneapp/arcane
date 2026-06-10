-- +goose Up
ALTER TABLE environments ADD COLUMN last_edge_transport TEXT;

-- +goose Down
ALTER TABLE environments DROP COLUMN last_edge_transport;

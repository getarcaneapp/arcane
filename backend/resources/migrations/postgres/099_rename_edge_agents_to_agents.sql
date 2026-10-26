-- +goose Up
ALTER TABLE environments RENAME COLUMN last_edge_transport TO last_agent_transport;
UPDATE environments SET last_agent_transport = NULL WHERE last_agent_transport = 'poll';
UPDATE environments SET api_url = 'agent://' || substr(api_url, 8) WHERE api_url LIKE 'edge://%';

-- +goose Down
UPDATE environments SET api_url = 'edge://' || substr(api_url, 9) WHERE api_url LIKE 'agent://%';
ALTER TABLE environments RENAME COLUMN last_agent_transport TO last_edge_transport;

-- +goose Up
ALTER TABLE activities ADD COLUMN batch_id TEXT;

CREATE INDEX idx_activities_environment_batch ON activities(environment_id, batch_id);

-- +goose Down
DROP INDEX IF EXISTS idx_activities_environment_batch;
ALTER TABLE activities DROP COLUMN batch_id;

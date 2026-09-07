-- +goose Up
UPDATE activities
SET environment_id = CAST(metadata AS jsonb)->>'environmentId'
WHERE type = 'job_run'
  AND jsonb_typeof(CAST(metadata AS jsonb)->'environmentId') = 'string'
  AND CAST(metadata AS jsonb)->>'environmentId' <> '';

-- +goose Down
UPDATE activities SET environment_id = '0' WHERE type = 'job_run';

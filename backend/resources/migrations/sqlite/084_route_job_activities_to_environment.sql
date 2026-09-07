-- +goose Up
UPDATE activities
SET environment_id = json_extract(metadata, '$.environmentId')
WHERE type = 'job_run'
  AND json_type(metadata, '$.environmentId') = 'text'
  AND json_extract(metadata, '$.environmentId') <> '';

-- +goose Down
UPDATE activities SET environment_id = '0' WHERE type = 'job_run';

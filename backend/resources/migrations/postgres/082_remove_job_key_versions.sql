-- +goose Up
UPDATE kv SET "key" = REPLACE("key", 'jobs.v1.', 'jobs.') WHERE "key" LIKE 'jobs.v1.%';
UPDATE kv SET "key" = REPLACE("key", 'jobs-catalog.v1/', 'jobs-catalog/') WHERE "key" LIKE 'jobs-catalog.v1/%';

-- +goose Down
UPDATE kv SET "key" = REPLACE("key", 'jobs.', 'jobs.v1.') WHERE "key" LIKE 'jobs.%';
UPDATE kv SET "key" = REPLACE("key", 'jobs-catalog/', 'jobs-catalog.v1/') WHERE "key" LIKE 'jobs-catalog/%';

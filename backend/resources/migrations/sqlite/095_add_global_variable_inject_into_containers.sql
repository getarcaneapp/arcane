-- +goose Up
ALTER TABLE global_variables ADD COLUMN inject_into_containers BOOLEAN NOT NULL DEFAULT false;
-- Existing variables were always injected into every container; keep that behavior.
UPDATE global_variables SET inject_into_containers = true;

-- +goose Down
ALTER TABLE global_variables DROP COLUMN inject_into_containers;

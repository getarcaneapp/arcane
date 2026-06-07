-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys=ON;
PRAGMA legacy_alter_table=ON;

ALTER TABLE users_table RENAME TO users;
ALTER TABLE image_update_table RENAME TO image_updates;
ALTER TABLE images_table RENAME TO images;
ALTER TABLE containers_table RENAME TO containers;
ALTER TABLE networks_table RENAME TO networks;
ALTER TABLE volumes_table RENAME TO volumes;
ALTER TABLE stacks_table RENAME TO stacks;

-- +goose Down
PRAGMA foreign_keys=ON;
PRAGMA legacy_alter_table=ON;

ALTER TABLE users RENAME TO users_table;
ALTER TABLE image_updates RENAME TO image_update_table;
ALTER TABLE images RENAME TO images_table;
ALTER TABLE containers RENAME TO containers_table;
ALTER TABLE networks RENAME TO networks_table;
ALTER TABLE volumes RENAME TO volumes_table;
ALTER TABLE stacks RENAME TO stacks_table;

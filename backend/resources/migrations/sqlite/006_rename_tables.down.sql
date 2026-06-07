PRAGMA foreign_keys=ON;
PRAGMA legacy_alter_table=ON;

ALTER TABLE users RENAME TO users_table;
ALTER TABLE image_updates RENAME TO image_update_table;
ALTER TABLE images RENAME TO images_table;
ALTER TABLE containers RENAME TO containers_table;
ALTER TABLE networks RENAME TO networks_table;
ALTER TABLE volumes RENAME TO volumes_table;
ALTER TABLE stacks RENAME TO stacks_table;
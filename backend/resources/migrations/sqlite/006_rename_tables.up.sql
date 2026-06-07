PRAGMA foreign_keys=ON;
PRAGMA legacy_alter_table=ON;

ALTER TABLE users_table RENAME TO users;
ALTER TABLE image_update_table RENAME TO image_updates;
ALTER TABLE images_table RENAME TO images;
ALTER TABLE containers_table RENAME TO containers;
ALTER TABLE networks_table RENAME TO networks;
ALTER TABLE volumes_table RENAME TO volumes;
ALTER TABLE stacks_table RENAME TO stacks;
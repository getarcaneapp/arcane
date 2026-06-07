ALTER TABLE IF EXISTS users_table RENAME TO users;
ALTER TABLE IF EXISTS image_update_table RENAME TO image_updates;
ALTER TABLE IF EXISTS images_table RENAME TO images;
ALTER TABLE IF EXISTS containers_table RENAME TO containers;
ALTER TABLE IF EXISTS networks_table RENAME TO networks;
ALTER TABLE IF EXISTS volumes_table RENAME TO volumes;
ALTER TABLE IF EXISTS stacks_table RENAME TO stacks;
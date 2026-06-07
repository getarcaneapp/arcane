ALTER TABLE IF EXISTS users RENAME TO users_table;
ALTER TABLE IF EXISTS image_updates RENAME TO image_update_table;
ALTER TABLE IF EXISTS images RENAME TO images_table;
ALTER TABLE IF EXISTS containers RENAME TO containers_table;
ALTER TABLE IF EXISTS networks RENAME TO networks_table;
ALTER TABLE IF EXISTS volumes RENAME TO volumes_table;
ALTER TABLE IF EXISTS stacks RENAME TO stacks_table;
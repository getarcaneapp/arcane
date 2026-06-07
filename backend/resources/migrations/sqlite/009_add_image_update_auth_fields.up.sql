ALTER TABLE image_updates ADD COLUMN auth_method TEXT;
ALTER TABLE image_updates ADD COLUMN auth_username TEXT;
ALTER TABLE image_updates ADD COLUMN auth_registry TEXT;
ALTER TABLE image_updates ADD COLUMN used_credential INTEGER DEFAULT 0;
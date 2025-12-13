ALTER TABLE IF EXISTS image_updates
    ADD COLUMN notification_sent BOOLEAN DEFAULT false;

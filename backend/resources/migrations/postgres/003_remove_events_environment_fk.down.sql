ALTER TABLE events ADD CONSTRAINT events_environment_id_fkey 
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL;
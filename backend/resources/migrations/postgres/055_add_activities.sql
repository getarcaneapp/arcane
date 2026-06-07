-- +goose Up
CREATE TABLE activities (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    environment_id TEXT NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    resource_type TEXT,
    resource_id TEXT,
    resource_name TEXT,
    progress INTEGER,
    step TEXT,
    latest_message TEXT,
    started_by_user_id TEXT,
    started_by_username TEXT,
    started_by_display_name TEXT,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    duration_ms BIGINT,
    error TEXT,
    metadata TEXT
);

CREATE INDEX idx_activities_environment_status_updated ON activities(environment_id, status, updated_at);
CREATE INDEX idx_activities_environment_started ON activities(environment_id, started_at);
CREATE INDEX idx_activities_type ON activities(type);
CREATE INDEX idx_activities_resource ON activities(resource_type, resource_id);
CREATE INDEX idx_activities_started_by_user_id ON activities(started_by_user_id);

CREATE TABLE activity_messages (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ,
    activity_id TEXT NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    payload TEXT
);

CREATE INDEX idx_activity_messages_activity_created ON activity_messages(activity_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS activity_messages;
DROP TABLE IF EXISTS activities;

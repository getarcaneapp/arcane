-- +goose Up
CREATE TABLE project_tags (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('ui', 'compose')),
    color TEXT NOT NULL DEFAULT 'gray' CHECK (color IN ('gray', 'purple', 'blue', 'green', 'yellow', 'orange', 'red', 'pink')),
    PRIMARY KEY (project_id, name, source)
);

CREATE INDEX idx_project_tags_name ON project_tags(name);

-- +goose Down
DROP TABLE IF EXISTS project_tags;

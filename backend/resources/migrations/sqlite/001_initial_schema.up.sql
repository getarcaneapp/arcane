CREATE TABLE IF NOT EXISTS settings (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'string',
    isPublic BOOLEAN DEFAULT FALSE NOT NULL,
    isInternal BOOLEAN DEFAULT FALSE NOT NULL,
    createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt DATETIME
);

CREATE TABLE IF NOT EXISTS users_table (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    email TEXT,
    roles TEXT NOT NULL DEFAULT '[]',
    require_password_change BOOLEAN NOT NULL DEFAULT false,
    oidc_subject_id TEXT,
    last_login DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS user_sessions_table (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_accessed DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    is_active BOOLEAN NOT NULL DEFAULT true,
    updated_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users_table(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS stacks_table (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dir_name TEXT UNIQUE,
    path TEXT NOT NULL,
    status TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    running_count INTEGER NOT NULL DEFAULT 0,
    auto_update BOOLEAN NOT NULL DEFAULT false,
    is_external BOOLEAN NOT NULL DEFAULT false,
    is_legacy BOOLEAN NOT NULL DEFAULT false,
    is_remote BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS environments (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    api_url TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'offline',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_seen DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS containers_table (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    status TEXT NOT NULL,
    state TEXT NOT NULL,
    ports TEXT,
    mounts TEXT,
    networks TEXT,
    labels TEXT,
    environment TEXT,
    command TEXT,
    stack_id TEXT,
    started_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (stack_id) REFERENCES stacks_table(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS images_table (
    id TEXT PRIMARY KEY,
    repo_tags TEXT,
    repo_digests TEXT,
    size INTEGER NOT NULL,
    virtual_size INTEGER NOT NULL DEFAULT 0,
    labels TEXT,
    created DATETIME NOT NULL,
    repo TEXT,
    tag TEXT,
    in_use BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS volumes_table (
    name TEXT PRIMARY KEY,
    driver TEXT NOT NULL,
    mountpoint TEXT NOT NULL,
    labels TEXT,
    scope TEXT NOT NULL,
    options TEXT,
    in_use BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS networks_table (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    driver TEXT NOT NULL,
    scope TEXT NOT NULL,
    internal BOOLEAN NOT NULL DEFAULT false,
    attachable BOOLEAN NOT NULL DEFAULT false,
    ingress BOOLEAN NOT NULL DEFAULT false,
    ipam TEXT,
    labels TEXT,
    options TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS image_update_table (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    tag TEXT NOT NULL,
    has_update BOOLEAN NOT NULL DEFAULT false,
    update_type TEXT,
    current_version TEXT NOT NULL,
    latest_version TEXT,
    current_digest TEXT,
    latest_digest TEXT,
    check_time DATETIME NOT NULL,
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS template_registries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS compose_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    content TEXT,
    env_content TEXT,
    is_custom BOOLEAN NOT NULL DEFAULT true,
    is_remote BOOLEAN NOT NULL DEFAULT false,
    registry_id TEXT,
    meta_version TEXT,
    meta_author TEXT,
    meta_tags TEXT,
    meta_remote_url TEXT,
    meta_env_url TEXT,
    meta_documentation_url TEXT,
    meta_icon_url TEXT,
    meta_updated_at TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (registry_id) REFERENCES template_registries(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS container_registries (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL,
    description TEXT,
    insecure BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE IF NOT EXISTS auto_update_records (
    id TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    status TEXT NOT NULL,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    update_available BOOLEAN NOT NULL DEFAULT false,
    update_applied BOOLEAN NOT NULL DEFAULT false,
    old_image_versions TEXT,
    new_image_versions TEXT,
    error TEXT,
    details TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_settings_key ON settings(key);
CREATE INDEX IF NOT EXISTS idx_settings_ispublic ON settings(isPublic);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions_table(user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_token ON user_sessions_table(token);
CREATE INDEX IF NOT EXISTS idx_containers_stack_id ON containers_table(stack_id);
CREATE INDEX IF NOT EXISTS idx_images_repo ON images_table(repo);
CREATE INDEX IF NOT EXISTS idx_images_tag ON images_table(tag);
CREATE INDEX IF NOT EXISTS idx_image_update_repository ON image_update_table(repository);
CREATE INDEX IF NOT EXISTS idx_image_update_tag ON image_update_table(tag);
CREATE INDEX IF NOT EXISTS idx_auto_update_resource_id ON auto_update_records(resource_id);
CREATE INDEX IF NOT EXISTS idx_auto_update_start_time ON auto_update_records(start_time);
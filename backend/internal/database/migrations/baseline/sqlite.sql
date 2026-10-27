CREATE TABLE settings (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'string',
    isPublic BOOLEAN DEFAULT FALSE NOT NULL,
    isInternal BOOLEAN DEFAULT FALSE NOT NULL,
    createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt DATETIME
);

CREATE TABLE "users" (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    email TEXT,
    roles TEXT NOT NULL DEFAULT '[]',
    oidc_subject_id TEXT,
    last_login DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, oidc_access_token TEXT, oidc_refresh_token TEXT, oidc_access_token_expires_at DATETIME, locale TEXT, requires_password_change BOOLEAN NOT NULL DEFAULT 0, is_service_account BOOLEAN NOT NULL DEFAULT 0, font_size INTEGER, has_avatar BOOLEAN NOT NULL DEFAULT 0, time_format TEXT NOT NULL DEFAULT 'auto'
    CHECK (time_format IN ('auto', '12h', '24h')), preferences TEXT, passkey_mfa_enabled BOOLEAN NOT NULL DEFAULT 0);

CREATE TABLE environments (
    id TEXT PRIMARY KEY,
    api_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'offline',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_seen DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, access_token TEXT, name TEXT DEFAULT '', api_key_id TEXT REFERENCES api_keys(id) ON DELETE SET NULL, is_edge INTEGER NOT NULL DEFAULT 0, hidden INTEGER NOT NULL DEFAULT 0, parent_environment_id TEXT, swarm_node_id TEXT, last_edge_transport TEXT);

CREATE TABLE "images" (
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

CREATE TABLE "image_updates" (
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
, auth_method TEXT, auth_username TEXT, auth_registry TEXT, used_credential INTEGER DEFAULT 0, notification_sent INTEGER DEFAULT 0, container_id TEXT NOT NULL DEFAULT '', image_id TEXT NOT NULL DEFAULT '', project_id TEXT NOT NULL DEFAULT '', service_name TEXT NOT NULL DEFAULT '', policy_key TEXT NOT NULL DEFAULT '', latest_seen_at DATETIME);

CREATE TABLE template_registries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE compose_templates (
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
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME, meta_registry_icon_url TEXT,
    FOREIGN KEY (registry_id) REFERENCES template_registries(id) ON DELETE SET NULL
);

CREATE TABLE auto_update_records (
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

CREATE TABLE "events" (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    title TEXT NOT NULL,
    description TEXT,
    resource_type TEXT,
    resource_id TEXT,
    resource_name TEXT,
    user_id TEXT,
    username TEXT,
    environment_id TEXT,
    metadata JSON,
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, deduplication_key TEXT);

CREATE TABLE notification_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider VARCHAR(50) NOT NULL,
    enabled BOOLEAN DEFAULT 0,
    config TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE git_repositories (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    auth_type TEXT NOT NULL,
    username TEXT,
    token TEXT,
    ssh_key TEXT,
    description TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, ssh_host_key_verification TEXT NOT NULL DEFAULT 'accept_new', commit_author_name TEXT NOT NULL DEFAULT '', commit_author_email TEXT NOT NULL DEFAULT '', signing_key TEXT NOT NULL DEFAULT '', signing_key_passphrase TEXT NOT NULL DEFAULT '');

CREATE TABLE volume_backups (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP
, status TEXT NOT NULL DEFAULT 'succeeded', trigger TEXT NOT NULL DEFAULT 'manual', s3_destination_id TEXT, error TEXT, destination TEXT NOT NULL DEFAULT 'local', local_snapshot_id TEXT, remote_snapshot_id TEXT, policy_id TEXT, format TEXT NOT NULL DEFAULT 'archive', remote_instance_id text NOT NULL DEFAULT '');

CREATE TABLE vulnerability_scans (
    id TEXT PRIMARY KEY,
    image_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    scan_time DATETIME NOT NULL,
    duration INTEGER NOT NULL DEFAULT 0,
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count INTEGER NOT NULL DEFAULT 0,
    medium_count INTEGER NOT NULL DEFAULT 0,
    low_count INTEGER NOT NULL DEFAULT 0,
    unknown_count INTEGER NOT NULL DEFAULT 0,
    total_count INTEGER NOT NULL DEFAULT 0,
    vulnerabilities TEXT,
    error TEXT,
    scanner_version TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, fixable_count INTEGER, scan_policy TEXT NOT NULL DEFAULT '');

CREATE TABLE vulnerability_ignores (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    image_id TEXT NOT NULL,
    vulnerability_id TEXT NOT NULL,
    pkg_name TEXT NOT NULL,
    installed_version TEXT NOT NULL,
    reason TEXT,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE image_builds (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    user_id TEXT,
    username TEXT,
    status TEXT NOT NULL,
    provider TEXT,
    context_dir TEXT NOT NULL,
    dockerfile TEXT,
    target TEXT,
    tags TEXT,
    platforms TEXT,
    build_args TEXT,
    labels TEXT,
    cache_from TEXT,
    cache_to TEXT,
    no_cache BOOLEAN NOT NULL DEFAULT 0,
    pull BOOLEAN NOT NULL DEFAULT 0,
    build_network TEXT,
    isolation TEXT,
    shm_size INTEGER,
    ulimits TEXT,
    entitlements TEXT,
    privileged BOOLEAN NOT NULL DEFAULT 0,
    extra_hosts TEXT,
    push BOOLEAN NOT NULL DEFAULT 0,
    load BOOLEAN NOT NULL DEFAULT 0,
    digest TEXT,
    error_message TEXT,
    output TEXT,
    output_truncated BOOLEAN NOT NULL DEFAULT 0,
    completed_at DATETIME,
    duration_ms INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE kv (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE "container_registries" (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    username TEXT NOT NULL,
    token TEXT NOT NULL,
    description TEXT,
    insecure BOOLEAN NOT NULL DEFAULT false,
    enabled BOOLEAN NOT NULL DEFAULT true,
    registry_type TEXT NOT NULL DEFAULT 'generic',
    aws_access_key_id TEXT,
    aws_secret_access_key TEXT,
    aws_region TEXT,
    ecr_token TEXT,
    ecr_token_generated_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, repository_names TEXT NOT NULL DEFAULT '[]');

CREATE TABLE "projects" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dir_name TEXT,
    path TEXT NOT NULL,
    status TEXT NOT NULL,
    service_count INTEGER NOT NULL DEFAULT 0,
    running_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status_reason TEXT,
    gitops_managed_by TEXT
, compose_project_name TEXT, is_archived BOOLEAN NOT NULL DEFAULT 0, archived_at DATETIME, image_refs_json TEXT NOT NULL DEFAULT '', build_image_refs_json TEXT);

CREATE TABLE webhooks (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    token_hash       TEXT NOT NULL UNIQUE,
    token_prefix     TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    action_type      TEXT NOT NULL DEFAULT '',
    target_id        TEXT NOT NULL,
    target_ref       TEXT NOT NULL DEFAULT '',
    environment_id   TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    last_triggered_at DATETIME,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME
);

CREATE TABLE "api_keys" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    managed_by TEXT,
    user_id TEXT,
    environment_id TEXT,
    expires_at DATETIME,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME, kind TEXT NOT NULL DEFAULT 'scoped',
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE TABLE user_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT,
    ip_address TEXT,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    last_used_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME
, source TEXT, federated_credential_id TEXT REFERENCES federated_credentials(id) ON DELETE SET NULL, mfa_method TEXT, mfa_verified_at DATETIME);

CREATE TABLE roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    permissions TEXT NOT NULL DEFAULT '[]',
    built_in    BOOLEAN NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME
);

CREATE TABLE user_role_assignments (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL,
    role_id        TEXT NOT NULL,
    environment_id TEXT,
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE TABLE api_key_permissions (
    id             TEXT PRIMARY KEY,
    api_key_id     TEXT NOT NULL,
    permission     TEXT NOT NULL,
    environment_id TEXT,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE TABLE oidc_role_mappings (
    id             TEXT PRIMARY KEY,
    claim_value    TEXT NOT NULL,
    role_id        TEXT NOT NULL,
    environment_id TEXT,
    -- 'manual' = created via UI/API; 'env' = declared via OIDC_ROLE_MAPPINGS
    -- env var and reconciled at boot. The API refuses mutations on 'env' rows.
    source         TEXT NOT NULL DEFAULT 'manual',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
);

CREATE TABLE activities (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
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
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    duration_ms INTEGER,
    error TEXT,
    metadata TEXT
, batch_id TEXT);

CREATE TABLE activity_messages (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    activity_id TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    payload TEXT,
    FOREIGN KEY(activity_id) REFERENCES activities(id) ON DELETE CASCADE
);

CREATE TABLE federated_credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT 0,
    issuer_url TEXT NOT NULL,
    audiences TEXT NOT NULL,
    subject_claim TEXT NOT NULL DEFAULT 'sub',
    subject_match TEXT NOT NULL,
    match_type TEXT NOT NULL DEFAULT 'exact',
    role_id TEXT NOT NULL,
    environment_id TEXT,
    identity_user_id TEXT NOT NULL,
    token_ttl_seconds INTEGER NOT NULL DEFAULT 900,
    last_used_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    FOREIGN KEY (role_id) REFERENCES roles(id),
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL,
    FOREIGN KEY (identity_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE federated_token_replays (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    issuer_url TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE environment_update_jobs (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    status TEXT NOT NULL,
    user_id TEXT,
    username TEXT,
    manager_version_at_start TEXT,
    manager_digest_at_start TEXT,
    manager_target_version TEXT,
    results TEXT,
    error TEXT,
    completed_at DATETIME
);

CREATE TABLE user_avatars (
    user_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    mime_type TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE global_variables (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL,
    updated_at DATETIME,
    key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    is_secret BOOLEAN NOT NULL DEFAULT FALSE,
    all_environments BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE global_variable_environments (
    global_variable_id TEXT NOT NULL REFERENCES global_variables(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    PRIMARY KEY (global_variable_id, environment_id)
);

CREATE TABLE passkeys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    credential_id BLOB NOT NULL,
    public_key BLOB NOT NULL,
    attestation_type TEXT,
    attestation_format TEXT,
    transports TEXT,
    aaguid BLOB,
    sign_count INTEGER NOT NULL DEFAULT 0,
    backup_eligible BOOLEAN NOT NULL DEFAULT 0,
    backup_state BOOLEAN NOT NULL DEFAULT 0,
    clone_warning BOOLEAN NOT NULL DEFAULT 0,
    authenticator_attachment TEXT,
    attestation_client_data_json BLOB,
    attestation_client_data_hash BLOB,
    attestation_authenticator_data BLOB,
    attestation_public_key_algorithm INTEGER,
    attestation_object BLOB,
    name TEXT NOT NULL,
    last_used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE auth_transactions (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT,
    source TEXT NOT NULL,
    user_agent TEXT,
    ip_address TEXT,
    secret_hash TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at DATETIME NOT NULL,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE passkey_ceremonies (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT,
    auth_transaction_id TEXT REFERENCES auth_transactions(id) ON DELETE CASCADE,
    rp_id TEXT NOT NULL,
    session_data TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    consumed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE passkey_recovery_codes (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL UNIQUE,
    used_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE project_tags (
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('ui', 'compose')),
    color TEXT NOT NULL DEFAULT 'gray' CHECK (color IN ('gray', 'purple', 'blue', 'green', 'yellow', 'orange', 'red', 'pink')),
    PRIMARY KEY (project_id, name, source),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE s3_destinations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT,
    bucket TEXT NOT NULL,
    region TEXT NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key TEXT NOT NULL,
    prefix TEXT,
    use_ssl INTEGER NOT NULL DEFAULT 1,
    force_path_style INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE volume_backup_policies (
    id TEXT PRIMARY KEY,
    volume_name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    stop_containers INTEGER NOT NULL DEFAULT 0,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE system_backup_runs (
    id TEXT PRIMARY KEY,
    size INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    status TEXT NOT NULL,
    trigger TEXT NOT NULL,
    destination TEXT NOT NULL,
    local_snapshot_id TEXT,
    remote_snapshot_id TEXT,
    s3_destination_id TEXT,
    policy_id TEXT,
    error TEXT
);

CREATE TABLE system_backup_policies (
    id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    schedule TEXT NOT NULL,
    retention_count INTEGER NOT NULL DEFAULT 7,
    local_enabled INTEGER NOT NULL DEFAULT 1,
    s3_enabled INTEGER NOT NULL DEFAULT 0,
    s3_destination_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE system_backup_recovery_config (
    id TEXT PRIMARY KEY,
    encrypted_recovery_key TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE image_patches (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    original_image_id TEXT NOT NULL,
    original_ref TEXT NOT NULL,
    original_digest TEXT,
    patched_ref TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'patching',
    packages_updated INTEGER,
    error TEXT,
    activity_id TEXT,
    duration_ms INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE vulnerability_reports (
    image_id TEXT PRIMARY KEY,
    data TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE apns_devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    events TEXT NOT NULL DEFAULT '{}',
    environment_ids TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    last_seen_at DATETIME
);

CREATE TABLE apns_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    envelope TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

CREATE TABLE vulnerability_scan_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    image_id TEXT NOT NULL,
    vulnerability_id TEXT NOT NULL,
    pkg_name TEXT NOT NULL,
    installed_version TEXT NOT NULL DEFAULT '',
    fixed_version TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL,
    severity_rank INTEGER NOT NULL DEFAULT 0,
    pkg_class TEXT NOT NULL DEFAULT '',
    pkg_type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    details TEXT, cvss_score REAL,
    FOREIGN KEY (image_id) REFERENCES vulnerability_scans(id) ON DELETE CASCADE
);

CREATE TABLE vulnerability_threat_intel (
    cve_id TEXT PRIMARY KEY,
    known_exploited BOOLEAN NOT NULL DEFAULT false,
    kev_date_added TEXT NOT NULL DEFAULT '',
    kev_due_date TEXT NOT NULL DEFAULT '',
    kev_ransomware BOOLEAN NOT NULL DEFAULT false,
    epss_score REAL,
    epss_percentile REAL,
    epss_checked_at DATETIME,
    updated_at DATETIME
);

CREATE TABLE vulnerability_risk_snapshots (
    snapshot_date TEXT PRIMARY KEY,
    risk_score INTEGER NOT NULL DEFAULT 0,
    risk_band TEXT NOT NULL DEFAULT 'none',
    critical_count INTEGER NOT NULL DEFAULT 0,
    high_count INTEGER NOT NULL DEFAULT 0,
    medium_count INTEGER NOT NULL DEFAULT 0,
    low_count INTEGER NOT NULL DEFAULT 0,
    unknown_count INTEGER NOT NULL DEFAULT 0,
    findings_count INTEGER NOT NULL DEFAULT 0,
    known_exploited_count INTEGER NOT NULL DEFAULT 0,
    overdue_known_exploited_count INTEGER NOT NULL DEFAULT 0,
    high_epss_count INTEGER NOT NULL DEFAULT 0,
    exposed_critical_high_count INTEGER NOT NULL DEFAULT 0,
    fixable_count INTEGER NOT NULL DEFAULT 0,
    images_scanned INTEGER NOT NULL DEFAULT 0,
    images_total INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
, scoring_version INTEGER NOT NULL DEFAULT 1, scored_images INTEGER NOT NULL DEFAULT 0);

CREATE TABLE "gitops_syncs" (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    branch TEXT NOT NULL,
    compose_path TEXT NOT NULL,
    project_name TEXT NOT NULL,
    project_id TEXT,
    auto_sync BOOLEAN NOT NULL DEFAULT false,
    sync_interval INTEGER NOT NULL DEFAULT 60,
    last_sync_at DATETIME,
    last_sync_status TEXT,
    last_sync_error TEXT,
    last_sync_commit TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    sync_directory INTEGER NOT NULL DEFAULT 1,
    synced_files TEXT,
    max_sync_files INTEGER NOT NULL DEFAULT 500,
    max_sync_total_size INTEGER NOT NULL DEFAULT 52428800,
    max_sync_binary_size INTEGER NOT NULL DEFAULT 10485760,
    target_type TEXT NOT NULL DEFAULT 'project',
    pre_deploy_script_path TEXT,
    pre_deploy_runner_image TEXT,
    pre_deploy_env TEXT,
    pre_deploy_extra_mounts TEXT,
    pre_deploy_timeout_sec INTEGER NOT NULL DEFAULT 60,
    pre_deploy_network_mode TEXT NOT NULL DEFAULT 'none',
    pre_deploy_last_run_at DATETIME,
    pre_deploy_last_run_status TEXT,
    pre_deploy_last_run_output TEXT,
    pull_image_after_sync BOOLEAN NOT NULL DEFAULT false,
    redeploy_after_sync BOOLEAN NOT NULL DEFAULT false,
    mode TEXT NOT NULL DEFAULT 'deploy',
    backup_directory TEXT NOT NULL DEFAULT '',
    backup_paths TEXT,
    backup_on_save BOOLEAN NOT NULL DEFAULT true,
    backup_pending BOOLEAN NOT NULL DEFAULT false,
    backup_pending_since DATETIME,
    backup_conflict BOOLEAN NOT NULL DEFAULT false,
    backup_failure_reason TEXT,
    last_backup_at DATETIME,
    last_backup_snapshot TEXT,
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE,
    FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE INDEX idx_settings_key ON settings(key);

CREATE INDEX idx_settings_ispublic ON settings(isPublic);

CREATE INDEX idx_images_repo ON "images"(repo);

CREATE INDEX idx_images_tag ON "images"(tag);

CREATE INDEX idx_auto_update_resource_id ON auto_update_records(resource_id);

CREATE INDEX idx_auto_update_start_time ON auto_update_records(start_time);

CREATE INDEX idx_events_type ON events(type);

CREATE INDEX idx_events_severity ON events(severity);

CREATE INDEX idx_events_resource_type ON events(resource_type);

CREATE INDEX idx_events_resource_id ON events(resource_id);

CREATE INDEX idx_events_user_id ON events(user_id);

CREATE INDEX idx_events_timestamp ON events(timestamp);

CREATE INDEX idx_environments_api_url ON environments(api_url);

CREATE INDEX idx_environments_name ON environments(name);

CREATE INDEX idx_notification_settings_provider ON notification_settings(provider);

CREATE UNIQUE INDEX idx_users_oidc_subject_id_unique
ON users(oidc_subject_id)
WHERE oidc_subject_id IS NOT NULL;

CREATE INDEX idx_environments_enabled ON environments(enabled);

CREATE INDEX idx_environments_status ON environments(status);

CREATE INDEX idx_environments_last_seen ON environments(last_seen);

CREATE INDEX idx_users_username ON users(username);

CREATE INDEX idx_users_email ON users(email);

CREATE INDEX idx_users_oidc_subject_id ON users(oidc_subject_id);

CREATE INDEX idx_image_updates_has_update ON image_updates(has_update);

CREATE INDEX idx_image_updates_has_update_notification ON image_updates(has_update, notification_sent);

CREATE INDEX idx_image_updates_check_time ON image_updates(check_time);

CREATE INDEX idx_image_updates_notification_sent ON image_updates(notification_sent);

CREATE INDEX idx_notification_settings_enabled ON notification_settings(enabled);

CREATE INDEX idx_git_repositories_enabled ON git_repositories(enabled);

CREATE INDEX idx_git_repositories_name ON git_repositories(name);

CREATE INDEX idx_vulnerability_scans_scan_time ON vulnerability_scans(scan_time);

CREATE INDEX idx_vulnerability_ignores_image ON vulnerability_ignores(image_id);

CREATE INDEX idx_vulnerability_ignores_composite ON vulnerability_ignores(environment_id, image_id, vulnerability_id, pkg_name, installed_version);

CREATE INDEX idx_image_builds_env ON image_builds(environment_id);

CREATE INDEX idx_image_builds_status ON image_builds(status);

CREATE INDEX idx_image_builds_created_at ON image_builds(created_at);

CREATE INDEX idx_container_registries_enabled ON container_registries(enabled);

CREATE INDEX idx_environments_access_token_not_null
ON environments(access_token)
WHERE access_token IS NOT NULL;

CREATE INDEX idx_environments_enabled_true
ON environments(id, name)
WHERE enabled = 1;

CREATE INDEX idx_git_repositories_enabled_url
ON git_repositories(enabled, url);

CREATE INDEX idx_git_repositories_auth_type
ON git_repositories(auth_type);

CREATE INDEX idx_compose_templates_lookup_name
ON compose_templates(is_remote, registry_id, name);

CREATE INDEX idx_compose_templates_lookup_description
ON compose_templates(is_remote, registry_id, description);

CREATE INDEX idx_volume_backups_volume_name_created_at
ON volume_backups(volume_name, created_at);

CREATE INDEX idx_image_builds_environment_created_at
ON image_builds(environment_id, created_at);

CREATE INDEX idx_image_builds_environment_status
ON image_builds(environment_id, status);

CREATE INDEX idx_events_environment_timestamp
ON events(environment_id, timestamp);

CREATE INDEX idx_image_updates_repository_tag
ON image_updates(repository, tag);

CREATE INDEX idx_vulnerability_scans_status_total_count
ON vulnerability_scans(status, total_count);

CREATE INDEX idx_vulnerability_ignores_env_created_at
ON vulnerability_ignores(environment_id, created_at);

CREATE INDEX idx_vulnerability_ignores_env_vulnerability_id
ON vulnerability_ignores(environment_id, vulnerability_id);

CREATE INDEX idx_environments_hidden ON environments(hidden);

CREATE INDEX idx_environments_parent_environment_id ON environments(parent_environment_id);

CREATE INDEX idx_environments_swarm_node_id ON environments(swarm_node_id);

CREATE INDEX idx_environments_parent_swarm_node ON environments(parent_environment_id, swarm_node_id);

CREATE INDEX idx_projects_status ON projects(status);

CREATE INDEX idx_projects_name ON projects(name);

CREATE INDEX idx_projects_gitops_managed_by ON projects(gitops_managed_by);

CREATE INDEX idx_projects_dir_name_not_null ON projects(dir_name) WHERE dir_name IS NOT NULL;

CREATE UNIQUE INDEX idx_projects_path_unique ON projects(path);

CREATE INDEX idx_webhooks_token_prefix ON webhooks(token_prefix);

CREATE INDEX idx_webhooks_environment_id ON webhooks(environment_id);

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);

CREATE INDEX idx_api_keys_key_prefix ON api_keys(key_prefix);

CREATE INDEX idx_api_keys_expires_at_not_null ON api_keys(expires_at) WHERE expires_at IS NOT NULL;

CREATE INDEX idx_api_keys_user_managed_by_created_at ON api_keys(user_id, managed_by, created_at, id);

CREATE INDEX idx_projects_is_archived ON projects(is_archived);

CREATE INDEX idx_user_sessions_user_id ON user_sessions(user_id);

CREATE INDEX idx_user_sessions_expires_at ON user_sessions(expires_at);

CREATE INDEX idx_ura_user ON user_role_assignments(user_id);

CREATE INDEX idx_ura_role ON user_role_assignments(role_id);

CREATE INDEX idx_ura_env ON user_role_assignments(environment_id);

CREATE INDEX idx_akp_key ON api_key_permissions(api_key_id);

CREATE UNIQUE INDEX idx_akp_uniq
    ON api_key_permissions(api_key_id, permission, COALESCE(environment_id, ''));

CREATE INDEX idx_orm_claim ON oidc_role_mappings(claim_value);

CREATE INDEX idx_activities_environment_status_updated ON activities(environment_id, status, updated_at);

CREATE INDEX idx_activities_environment_started ON activities(environment_id, started_at);

CREATE INDEX idx_activities_type ON activities(type);

CREATE INDEX idx_activities_resource ON activities(resource_type, resource_id);

CREATE INDEX idx_activities_started_by_user_id ON activities(started_by_user_id);

CREATE INDEX idx_activity_messages_activity_created ON activity_messages(activity_id, created_at);

CREATE INDEX idx_federated_credentials_issuer_url ON federated_credentials(issuer_url);

CREATE INDEX idx_federated_credentials_enabled ON federated_credentials(enabled);

CREATE INDEX idx_federated_credentials_identity_user_id ON federated_credentials(identity_user_id);

CREATE INDEX idx_federated_credentials_role_id ON federated_credentials(role_id);

CREATE INDEX idx_federated_token_replays_issuer_url ON federated_token_replays(issuer_url);

CREATE INDEX idx_federated_token_replays_expires_at ON federated_token_replays(expires_at);

CREATE INDEX idx_user_sessions_federated_credential_id ON user_sessions(federated_credential_id);

CREATE INDEX idx_environment_update_jobs_status ON environment_update_jobs(status);

CREATE INDEX idx_global_variables_key ON global_variables(key);

CREATE INDEX idx_global_variable_environments_env ON global_variable_environments(environment_id);

CREATE INDEX idx_activities_environment_batch ON activities(environment_id, batch_id);

CREATE UNIQUE INDEX idx_passkeys_rp_credential ON passkeys(rp_id, credential_id);

CREATE INDEX idx_passkeys_user_id ON passkeys(user_id);

CREATE INDEX idx_auth_transactions_user_id ON auth_transactions(user_id);

CREATE INDEX idx_auth_transactions_status ON auth_transactions(status);

CREATE INDEX idx_auth_transactions_secret_hash ON auth_transactions(secret_hash);

CREATE INDEX idx_auth_transactions_expires_at ON auth_transactions(expires_at);

CREATE INDEX idx_passkey_ceremonies_user_id ON passkey_ceremonies(user_id);

CREATE INDEX idx_passkey_ceremonies_transaction_id ON passkey_ceremonies(auth_transaction_id);

CREATE INDEX idx_passkey_ceremonies_expires_at ON passkey_ceremonies(expires_at);

CREATE INDEX idx_passkey_recovery_codes_user_id ON passkey_recovery_codes(user_id);

CREATE INDEX idx_passkey_recovery_codes_used_at ON passkey_recovery_codes(used_at);

CREATE INDEX idx_project_tags_name ON project_tags(name);

CREATE INDEX idx_volume_backups_s3_destination_id ON volume_backups(s3_destination_id);

CREATE INDEX idx_volume_backups_policy_id ON volume_backups(policy_id);

CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies(volume_name);

CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies(s3_destination_id);

CREATE INDEX idx_system_backup_runs_s3_destination_id ON system_backup_runs(s3_destination_id);

CREATE INDEX idx_system_backup_runs_policy_id ON system_backup_runs(policy_id);

CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies(s3_destination_id);

CREATE INDEX idx_image_patches_env_image ON image_patches(environment_id, original_image_id);

CREATE INDEX idx_image_patches_created_at ON image_patches(created_at);

CREATE UNIQUE INDEX idx_apns_devices_recipient ON apns_devices(recipient_id);

CREATE INDEX idx_apns_devices_user ON apns_devices(user_id);

CREATE INDEX idx_apns_outbox_next_attempt ON apns_outbox(next_attempt_at);

CREATE INDEX idx_vulnerability_scan_items_image_id ON vulnerability_scan_items(image_id);

CREATE INDEX idx_vulnerability_scan_items_severity ON vulnerability_scan_items(severity);

CREATE INDEX idx_vulnerability_scan_items_vulnerability_id ON vulnerability_scan_items(vulnerability_id);

CREATE INDEX idx_vulnerability_scan_items_severity_rank_id ON vulnerability_scan_items(severity_rank, id);

CREATE INDEX idx_image_updates_container_id ON image_updates(container_id);

CREATE INDEX idx_image_updates_image_id ON image_updates(image_id);

CREATE INDEX idx_image_updates_project_id ON image_updates(project_id);

CREATE INDEX idx_vulnerability_threat_intel_known_exploited ON vulnerability_threat_intel(known_exploited);

CREATE UNIQUE INDEX idx_events_deduplication_key ON events (deduplication_key);

CREATE UNIQUE INDEX idx_ura_uniq
    ON user_role_assignments(user_id, role_id, COALESCE(environment_id, ''), source);

CREATE INDEX idx_gitops_syncs_auto_sync_true
ON gitops_syncs(id, environment_id, sync_interval, last_sync_at)
WHERE auto_sync = 1;

CREATE UNIQUE INDEX idx_gitops_syncs_backup_project ON gitops_syncs(project_id) WHERE mode = 'backup';

CREATE INDEX idx_gitops_syncs_environment_auto_sync
ON gitops_syncs(environment_id, auto_sync);

CREATE INDEX idx_gitops_syncs_environment_id ON gitops_syncs(environment_id);

CREATE INDEX idx_gitops_syncs_environment_last_sync_status
ON gitops_syncs(environment_id, last_sync_status);

CREATE INDEX idx_gitops_syncs_environment_project_id
ON gitops_syncs(environment_id, project_id);

CREATE INDEX idx_gitops_syncs_environment_repository_id
ON gitops_syncs(environment_id, repository_id);

CREATE INDEX idx_gitops_syncs_project_id ON gitops_syncs(project_id);

CREATE INDEX idx_gitops_syncs_repository_id ON gitops_syncs(repository_id);

CREATE INDEX idx_gitops_syncs_sync_directory ON gitops_syncs(sync_directory);

CREATE INDEX idx_gitops_syncs_auto_sync ON gitops_syncs(auto_sync);

CREATE INDEX idx_gitops_syncs_last_sync_commit ON gitops_syncs(last_sync_commit);

INSERT INTO settings (key, value) VALUES
    ('vulnerabilityScanEnabled', 'false'),
    ('vulnerabilityScanInterval', '0 0 0 * * *'),
    ('autoUpdateExcludedContainers', '');

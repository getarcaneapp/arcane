CREATE TABLE activities (
    id text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    environment_id text NOT NULL,
    type text NOT NULL,
    status text NOT NULL,
    resource_type text,
    resource_id text,
    resource_name text,
    progress integer,
    step text,
    latest_message text,
    started_by_user_id text,
    started_by_username text,
    started_by_display_name text,
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    duration_ms bigint,
    error text,
    metadata text,
    batch_id text
);

CREATE TABLE activity_messages (
    id text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    activity_id text NOT NULL,
    level text NOT NULL,
    message text NOT NULL,
    payload text
);

CREATE TABLE api_key_permissions (
    id text NOT NULL,
    api_key_id text NOT NULL,
    permission text NOT NULL,
    environment_id text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE api_keys (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    key_hash text NOT NULL,
    key_prefix text NOT NULL,
    user_id text,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    environment_id text,
    managed_by text,
    kind text DEFAULT 'scoped'::text NOT NULL
);

CREATE TABLE apns_devices (
    id text NOT NULL,
    user_id text NOT NULL,
    recipient_id text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    events jsonb DEFAULT '{}'::jsonb NOT NULL,
    environment_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    last_seen_at timestamp with time zone
);

CREATE TABLE apns_outbox (
    id text NOT NULL,
    event_id text NOT NULL,
    envelope text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone NOT NULL,
    last_error text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE auth_transactions (
    id text NOT NULL,
    kind text NOT NULL,
    user_id text NOT NULL,
    session_id text,
    source text NOT NULL,
    user_agent text,
    ip_address text,
    secret_hash text,
    status text DEFAULT 'pending'::text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE auto_update_records (
    id text NOT NULL,
    resource_id text NOT NULL,
    resource_type text NOT NULL,
    resource_name text NOT NULL,
    status text NOT NULL,
    start_time timestamp with time zone NOT NULL,
    end_time timestamp with time zone,
    update_available boolean DEFAULT false NOT NULL,
    update_applied boolean DEFAULT false NOT NULL,
    old_image_versions jsonb,
    new_image_versions jsonb,
    error text,
    details jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE compose_templates (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    content text,
    env_content text,
    is_custom boolean DEFAULT true NOT NULL,
    is_remote boolean DEFAULT false NOT NULL,
    registry_id text,
    meta_version text,
    meta_author text,
    meta_tags text,
    meta_remote_url text,
    meta_env_url text,
    meta_documentation_url text,
    meta_icon_url text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    meta_registry_icon_url text
);

CREATE TABLE container_registries (
    id text NOT NULL,
    url text NOT NULL,
    username text NOT NULL,
    token text NOT NULL,
    description text,
    insecure boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    registry_type text DEFAULT 'generic'::text NOT NULL,
    aws_access_key_id text,
    aws_secret_access_key text,
    aws_region text,
    ecr_token text,
    ecr_token_generated_at timestamp with time zone,
    repository_names text DEFAULT '[]'::text NOT NULL
);

CREATE TABLE environment_update_jobs (
    id text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    status text NOT NULL,
    user_id text,
    username text,
    manager_version_at_start text,
    manager_digest_at_start text,
    manager_target_version text,
    results text,
    error text,
    completed_at timestamp with time zone
);

CREATE TABLE environments (
    id text NOT NULL,
    api_url text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_seen timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    access_token text,
    name text,
    api_key_id text,
    is_edge boolean DEFAULT false NOT NULL,
    hidden boolean DEFAULT false NOT NULL,
    parent_environment_id text,
    swarm_node_id text,
    last_edge_transport text
);

CREATE TABLE events (
    id text NOT NULL,
    type text NOT NULL,
    severity text DEFAULT 'info'::text NOT NULL,
    title text NOT NULL,
    description text,
    resource_type text,
    resource_id text,
    resource_name text,
    user_id text,
    username text,
    environment_id text,
    metadata jsonb,
    "timestamp" timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    deduplication_key text
);

CREATE TABLE federated_credentials (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    enabled boolean DEFAULT false NOT NULL,
    issuer_url text NOT NULL,
    audiences text NOT NULL,
    subject_claim text DEFAULT 'sub'::text NOT NULL,
    subject_match text NOT NULL,
    match_type text DEFAULT 'exact'::text NOT NULL,
    role_id text NOT NULL,
    environment_id text,
    identity_user_id text NOT NULL,
    token_ttl_seconds integer DEFAULT 900 NOT NULL,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE federated_token_replays (
    id text NOT NULL,
    token_hash text NOT NULL,
    issuer_url text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE git_repositories (
    id text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    auth_type text NOT NULL,
    username text,
    token text,
    ssh_key text,
    description text,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    ssh_host_key_verification text DEFAULT 'accept_new'::text NOT NULL,
    commit_author_name text DEFAULT ''::text NOT NULL,
    commit_author_email text DEFAULT ''::text NOT NULL,
    signing_key text DEFAULT ''::text NOT NULL,
    signing_key_passphrase text DEFAULT ''::text NOT NULL
);

CREATE TABLE gitops_syncs (
    id text NOT NULL,
    name text NOT NULL,
    repository_id text NOT NULL,
    branch text NOT NULL,
    compose_path text NOT NULL,
    project_name text NOT NULL,
    project_id text,
    auto_sync boolean DEFAULT false NOT NULL,
    sync_interval integer DEFAULT 60 NOT NULL,
    last_sync_at timestamp with time zone,
    last_sync_status text,
    last_sync_error text,
    last_sync_commit text,
    environment_id text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    sync_directory boolean DEFAULT true NOT NULL,
    synced_files text,
    max_sync_files integer DEFAULT 500 NOT NULL,
    max_sync_total_size bigint DEFAULT 52428800 NOT NULL,
    max_sync_binary_size bigint DEFAULT 10485760 NOT NULL,
    target_type character varying(255) DEFAULT 'project'::character varying NOT NULL,
    pre_deploy_script_path text,
    pre_deploy_runner_image text,
    pre_deploy_env text,
    pre_deploy_extra_mounts text,
    pre_deploy_timeout_sec integer DEFAULT 60 NOT NULL,
    pre_deploy_network_mode text DEFAULT 'none'::text NOT NULL,
    pre_deploy_last_run_at timestamp with time zone,
    pre_deploy_last_run_status text,
    pre_deploy_last_run_output text,
    pull_image_after_sync boolean DEFAULT false NOT NULL,
    redeploy_after_sync boolean DEFAULT false NOT NULL,
    mode text DEFAULT 'deploy'::text NOT NULL,
    backup_directory text DEFAULT ''::text NOT NULL,
    backup_paths text,
    backup_on_save boolean DEFAULT true NOT NULL,
    backup_pending boolean DEFAULT false NOT NULL,
    backup_pending_since timestamp with time zone,
    backup_conflict boolean DEFAULT false NOT NULL,
    backup_failure_reason text,
    last_backup_at timestamp with time zone,
    last_backup_snapshot text
);

CREATE TABLE global_variable_environments (
    global_variable_id text NOT NULL,
    environment_id text NOT NULL
);

CREATE TABLE global_variables (
    id text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    key text NOT NULL,
    value text DEFAULT ''::text NOT NULL,
    is_secret boolean DEFAULT false NOT NULL,
    all_environments boolean DEFAULT true NOT NULL
);

CREATE TABLE image_builds (
    id text NOT NULL,
    environment_id text NOT NULL,
    user_id text,
    username text,
    status text NOT NULL,
    provider text,
    context_dir text NOT NULL,
    dockerfile text,
    target text,
    tags text,
    platforms text,
    build_args text,
    labels text,
    cache_from text,
    cache_to text,
    no_cache boolean DEFAULT false NOT NULL,
    pull boolean DEFAULT false NOT NULL,
    build_network text,
    isolation text,
    shm_size bigint,
    ulimits text,
    entitlements text,
    privileged boolean DEFAULT false NOT NULL,
    extra_hosts text,
    push boolean DEFAULT false NOT NULL,
    load boolean DEFAULT false NOT NULL,
    digest text,
    error_message text,
    output text,
    output_truncated boolean DEFAULT false NOT NULL,
    completed_at timestamp without time zone,
    duration_ms bigint,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone
);

CREATE TABLE image_patches (
    id text NOT NULL,
    environment_id text NOT NULL,
    original_image_id text NOT NULL,
    original_ref text NOT NULL,
    original_digest text,
    patched_ref text NOT NULL,
    mode text NOT NULL,
    status text DEFAULT 'patching'::text NOT NULL,
    packages_updated integer,
    error text,
    activity_id text,
    duration_ms bigint,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone
);

CREATE TABLE image_updates (
    id text NOT NULL,
    repository text NOT NULL,
    tag text NOT NULL,
    has_update boolean DEFAULT false NOT NULL,
    update_type text,
    current_version text NOT NULL,
    latest_version text,
    current_digest text,
    latest_digest text,
    check_time timestamp with time zone NOT NULL,
    response_time_ms integer DEFAULT 0 NOT NULL,
    last_error text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    auth_method text,
    auth_username text,
    auth_registry text,
    used_credential boolean DEFAULT false,
    notification_sent boolean DEFAULT false,
    container_id text DEFAULT ''::text NOT NULL,
    image_id text DEFAULT ''::text NOT NULL,
    project_id text DEFAULT ''::text NOT NULL,
    service_name text DEFAULT ''::text NOT NULL,
    policy_key text DEFAULT ''::text NOT NULL,
    latest_seen_at timestamp with time zone
);

CREATE TABLE images (
    id text NOT NULL,
    repo_tags jsonb,
    repo_digests jsonb,
    size bigint NOT NULL,
    virtual_size bigint DEFAULT 0 NOT NULL,
    labels jsonb,
    created timestamp with time zone NOT NULL,
    repo text,
    tag text,
    in_use boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE kv (
    key text NOT NULL,
    value text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE notification_settings (
    id integer NOT NULL,
    provider character varying(50) NOT NULL,
    enabled boolean DEFAULT false,
    config jsonb NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE SEQUENCE notification_settings_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE notification_settings_id_seq OWNED BY notification_settings.id;

CREATE TABLE oidc_role_mappings (
    id text NOT NULL,
    claim_value text NOT NULL,
    role_id text NOT NULL,
    environment_id text,
    source text DEFAULT 'manual'::text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE passkey_ceremonies (
    id text NOT NULL,
    purpose text NOT NULL,
    user_id text,
    session_id text,
    auth_transaction_id text,
    rp_id text NOT NULL,
    session_data text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE passkey_recovery_codes (
    id text NOT NULL,
    user_id text NOT NULL,
    code_hash text NOT NULL,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE passkeys (
    id text NOT NULL,
    user_id text NOT NULL,
    rp_id text NOT NULL,
    credential_id bytea NOT NULL,
    public_key bytea NOT NULL,
    attestation_type text,
    attestation_format text,
    transports text,
    aaguid bytea,
    sign_count integer DEFAULT 0 NOT NULL,
    backup_eligible boolean DEFAULT false NOT NULL,
    backup_state boolean DEFAULT false NOT NULL,
    clone_warning boolean DEFAULT false NOT NULL,
    authenticator_attachment text,
    attestation_client_data_json bytea,
    attestation_client_data_hash bytea,
    attestation_authenticator_data bytea,
    attestation_public_key_algorithm bigint,
    attestation_object bytea,
    name text NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE project_tags (
    project_id text NOT NULL,
    name text NOT NULL,
    source text NOT NULL,
    color text DEFAULT 'gray'::text NOT NULL,
    CONSTRAINT project_tags_color_check CHECK ((color = ANY (ARRAY['gray'::text, 'purple'::text, 'blue'::text, 'green'::text, 'yellow'::text, 'orange'::text, 'red'::text, 'pink'::text]))),
    CONSTRAINT project_tags_source_check CHECK ((source = ANY (ARRAY['ui'::text, 'compose'::text])))
);

CREATE TABLE projects (
    id text NOT NULL,
    name text NOT NULL,
    dir_name text,
    path text NOT NULL,
    status text NOT NULL,
    service_count integer DEFAULT 0 NOT NULL,
    running_count integer DEFAULT 0 NOT NULL,
    auto_update boolean DEFAULT false NOT NULL,
    is_external boolean DEFAULT false NOT NULL,
    is_legacy boolean DEFAULT false NOT NULL,
    is_remote boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    status_reason text,
    gitops_managed_by text,
    compose_project_name text,
    is_archived boolean DEFAULT false NOT NULL,
    archived_at timestamp without time zone,
    image_refs_json text DEFAULT ''::text NOT NULL,
    build_image_refs_json text
);

CREATE TABLE roles (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    permissions jsonb DEFAULT '[]'::jsonb NOT NULL,
    built_in boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE s3_destinations (
    id text NOT NULL,
    name text NOT NULL,
    endpoint text,
    bucket text NOT NULL,
    region text NOT NULL,
    access_key_id text NOT NULL,
    secret_access_key text NOT NULL,
    prefix text,
    use_ssl boolean DEFAULT true NOT NULL,
    force_path_style boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE settings (
    key text NOT NULL,
    value text NOT NULL,
    type text DEFAULT 'string'::text NOT NULL,
    ispublic boolean DEFAULT false NOT NULL,
    isinternal boolean DEFAULT false NOT NULL,
    createdat timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updatedat timestamp with time zone
);

CREATE TABLE system_backup_policies (
    id text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    schedule text NOT NULL,
    retention_count integer DEFAULT 7 NOT NULL,
    local_enabled boolean DEFAULT true NOT NULL,
    s3_enabled boolean DEFAULT false NOT NULL,
    s3_destination_id text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE system_backup_recovery_config (
    id text NOT NULL,
    encrypted_recovery_key text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE system_backup_runs (
    id text NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    status text NOT NULL,
    trigger text NOT NULL,
    destination text NOT NULL,
    local_snapshot_id text,
    remote_snapshot_id text,
    s3_destination_id text,
    policy_id text,
    error text
);

CREATE TABLE template_registries (
    id text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE user_avatars (
    user_id text NOT NULL,
    data bytea NOT NULL,
    mime_type text NOT NULL
);

CREATE TABLE user_role_assignments (
    id text NOT NULL,
    user_id text NOT NULL,
    role_id text NOT NULL,
    environment_id text,
    source text DEFAULT 'manual'::text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE user_sessions (
    id text NOT NULL,
    user_id text NOT NULL,
    refresh_token_hash text NOT NULL,
    user_agent text,
    ip_address text,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone,
    last_used_at timestamp without time zone NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    revoked_at timestamp without time zone,
    source text,
    federated_credential_id text,
    mfa_method text,
    mfa_verified_at timestamp with time zone
);

CREATE TABLE users (
    id text NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    display_name text,
    email text,
    roles jsonb DEFAULT '[]'::jsonb NOT NULL,
    oidc_subject_id text,
    last_login timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    oidc_access_token text,
    oidc_refresh_token text,
    oidc_access_token_expires_at timestamp with time zone,
    locale text,
    requires_password_change boolean DEFAULT false NOT NULL,
    is_service_account boolean DEFAULT false NOT NULL,
    font_size integer,
    has_avatar boolean DEFAULT false NOT NULL,
    time_format text DEFAULT 'auto'::text NOT NULL,
    preferences text,
    passkey_mfa_enabled boolean DEFAULT false NOT NULL,
    CONSTRAINT users_time_format_check CHECK ((time_format = ANY (ARRAY['auto'::text, '12h'::text, '24h'::text])))
);

CREATE TABLE volume_backup_policies (
    id text NOT NULL,
    volume_name text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    schedule text NOT NULL,
    retention_count integer DEFAULT 7 NOT NULL,
    stop_containers boolean DEFAULT false NOT NULL,
    local_enabled boolean DEFAULT true NOT NULL,
    s3_enabled boolean DEFAULT false NOT NULL,
    s3_destination_id text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone
);

CREATE TABLE volume_backups (
    id text NOT NULL,
    volume_name text NOT NULL,
    size bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone,
    status text DEFAULT 'succeeded'::text NOT NULL,
    trigger text DEFAULT 'manual'::text NOT NULL,
    s3_destination_id text,
    error text,
    destination text DEFAULT 'local'::text NOT NULL,
    local_snapshot_id text,
    remote_snapshot_id text,
    policy_id text,
    format text DEFAULT 'archive'::text NOT NULL,
    remote_instance_id text DEFAULT ''::text NOT NULL
);

CREATE TABLE vulnerability_ignores (
    id text NOT NULL,
    environment_id text NOT NULL,
    image_id text NOT NULL,
    vulnerability_id text NOT NULL,
    pkg_name text NOT NULL,
    installed_version text NOT NULL,
    reason text,
    created_by text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE vulnerability_reports (
    image_id text NOT NULL,
    data text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone
);

CREATE TABLE vulnerability_risk_snapshots (
    snapshot_date text NOT NULL,
    risk_score integer DEFAULT 0 NOT NULL,
    risk_band text DEFAULT 'none'::text NOT NULL,
    critical_count integer DEFAULT 0 NOT NULL,
    high_count integer DEFAULT 0 NOT NULL,
    medium_count integer DEFAULT 0 NOT NULL,
    low_count integer DEFAULT 0 NOT NULL,
    unknown_count integer DEFAULT 0 NOT NULL,
    findings_count integer DEFAULT 0 NOT NULL,
    known_exploited_count integer DEFAULT 0 NOT NULL,
    overdue_known_exploited_count integer DEFAULT 0 NOT NULL,
    high_epss_count integer DEFAULT 0 NOT NULL,
    exposed_critical_high_count integer DEFAULT 0 NOT NULL,
    fixable_count integer DEFAULT 0 NOT NULL,
    images_scanned integer DEFAULT 0 NOT NULL,
    images_total integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone,
    scoring_version integer DEFAULT 1 NOT NULL,
    scored_images integer DEFAULT 0 NOT NULL
);

CREATE TABLE vulnerability_scan_items (
    id bigint NOT NULL,
    image_id text NOT NULL,
    vulnerability_id text NOT NULL,
    pkg_name text NOT NULL,
    installed_version text DEFAULT ''::text NOT NULL,
    fixed_version text DEFAULT ''::text NOT NULL,
    severity text NOT NULL,
    severity_rank integer DEFAULT 0 NOT NULL,
    pkg_class text DEFAULT ''::text NOT NULL,
    pkg_type text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    details text,
    cvss_score double precision
);

CREATE SEQUENCE vulnerability_scan_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE vulnerability_scan_items_id_seq OWNED BY vulnerability_scan_items.id;

CREATE TABLE vulnerability_scans (
    id text NOT NULL,
    image_name text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    scan_time timestamp without time zone NOT NULL,
    duration bigint DEFAULT 0 NOT NULL,
    critical_count integer DEFAULT 0 NOT NULL,
    high_count integer DEFAULT 0 NOT NULL,
    medium_count integer DEFAULT 0 NOT NULL,
    low_count integer DEFAULT 0 NOT NULL,
    unknown_count integer DEFAULT 0 NOT NULL,
    total_count integer DEFAULT 0 NOT NULL,
    vulnerabilities text,
    error text,
    scanner_version text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone,
    fixable_count integer,
    scan_policy text DEFAULT ''::text NOT NULL
);

CREATE TABLE vulnerability_threat_intel (
    cve_id text NOT NULL,
    known_exploited boolean DEFAULT false NOT NULL,
    kev_date_added text DEFAULT ''::text NOT NULL,
    kev_due_date text DEFAULT ''::text NOT NULL,
    kev_ransomware boolean DEFAULT false NOT NULL,
    epss_score double precision,
    epss_percentile double precision,
    epss_checked_at timestamp with time zone,
    updated_at timestamp with time zone
);

CREATE TABLE webhooks (
    id text NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    target_type text NOT NULL,
    action_type text DEFAULT ''::text NOT NULL,
    target_id text NOT NULL,
    target_ref text DEFAULT ''::text NOT NULL,
    environment_id text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_triggered_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone
);

ALTER TABLE ONLY notification_settings ALTER COLUMN id SET DEFAULT nextval('notification_settings_id_seq'::regclass);

ALTER TABLE ONLY vulnerability_scan_items ALTER COLUMN id SET DEFAULT nextval('vulnerability_scan_items_id_seq'::regclass);

ALTER TABLE ONLY activities
    ADD CONSTRAINT activities_pkey PRIMARY KEY (id);

ALTER TABLE ONLY activity_messages
    ADD CONSTRAINT activity_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY api_key_permissions
    ADD CONSTRAINT api_key_permissions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);

ALTER TABLE ONLY apns_devices
    ADD CONSTRAINT apns_devices_pkey PRIMARY KEY (id);

ALTER TABLE ONLY apns_outbox
    ADD CONSTRAINT apns_outbox_pkey PRIMARY KEY (id);

ALTER TABLE ONLY auth_transactions
    ADD CONSTRAINT auth_transactions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY auto_update_records
    ADD CONSTRAINT auto_update_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY compose_templates
    ADD CONSTRAINT compose_templates_pkey PRIMARY KEY (id);

ALTER TABLE ONLY container_registries
    ADD CONSTRAINT container_registries_pkey PRIMARY KEY (id);

ALTER TABLE ONLY environment_update_jobs
    ADD CONSTRAINT environment_update_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY environments
    ADD CONSTRAINT environments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY events
    ADD CONSTRAINT events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY federated_credentials
    ADD CONSTRAINT federated_credentials_pkey PRIMARY KEY (id);

ALTER TABLE ONLY federated_token_replays
    ADD CONSTRAINT federated_token_replays_pkey PRIMARY KEY (id);

ALTER TABLE ONLY federated_token_replays
    ADD CONSTRAINT federated_token_replays_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY git_repositories
    ADD CONSTRAINT git_repositories_pkey PRIMARY KEY (id);

ALTER TABLE ONLY gitops_syncs
    ADD CONSTRAINT gitops_syncs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY global_variable_environments
    ADD CONSTRAINT global_variable_environments_pkey PRIMARY KEY (global_variable_id, environment_id);

ALTER TABLE ONLY global_variables
    ADD CONSTRAINT global_variables_pkey PRIMARY KEY (id);

ALTER TABLE ONLY image_builds
    ADD CONSTRAINT image_builds_pkey PRIMARY KEY (id);

ALTER TABLE ONLY image_patches
    ADD CONSTRAINT image_patches_pkey PRIMARY KEY (id);

ALTER TABLE ONLY image_updates
    ADD CONSTRAINT image_update_table_pkey PRIMARY KEY (id);

ALTER TABLE ONLY images
    ADD CONSTRAINT images_table_pkey PRIMARY KEY (id);

ALTER TABLE ONLY kv
    ADD CONSTRAINT kv_pkey PRIMARY KEY (key);

ALTER TABLE ONLY notification_settings
    ADD CONSTRAINT notification_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY oidc_role_mappings
    ADD CONSTRAINT oidc_role_mappings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY passkey_ceremonies
    ADD CONSTRAINT passkey_ceremonies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY passkey_recovery_codes
    ADD CONSTRAINT passkey_recovery_codes_code_hash_key UNIQUE (code_hash);

ALTER TABLE ONLY passkey_recovery_codes
    ADD CONSTRAINT passkey_recovery_codes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY passkeys
    ADD CONSTRAINT passkeys_pkey PRIMARY KEY (id);

ALTER TABLE ONLY project_tags
    ADD CONSTRAINT project_tags_pkey PRIMARY KEY (project_id, name, source);

ALTER TABLE ONLY roles
    ADD CONSTRAINT roles_name_key UNIQUE (name);

ALTER TABLE ONLY roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY s3_destinations
    ADD CONSTRAINT s3_destinations_name_key UNIQUE (name);

ALTER TABLE ONLY s3_destinations
    ADD CONSTRAINT s3_destinations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (key);

ALTER TABLE ONLY projects
    ADD CONSTRAINT stacks_table_dir_name_key UNIQUE (dir_name);

ALTER TABLE ONLY projects
    ADD CONSTRAINT stacks_table_pkey PRIMARY KEY (id);

ALTER TABLE ONLY system_backup_policies
    ADD CONSTRAINT system_backup_policies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY system_backup_recovery_config
    ADD CONSTRAINT system_backup_recovery_config_pkey PRIMARY KEY (id);

ALTER TABLE ONLY system_backup_runs
    ADD CONSTRAINT system_backup_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY template_registries
    ADD CONSTRAINT template_registries_pkey PRIMARY KEY (id);

ALTER TABLE ONLY template_registries
    ADD CONSTRAINT template_registries_url_key UNIQUE (url);

ALTER TABLE ONLY user_avatars
    ADD CONSTRAINT user_avatars_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY user_role_assignments
    ADD CONSTRAINT user_role_assignments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY user_sessions
    ADD CONSTRAINT user_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY user_sessions
    ADD CONSTRAINT user_sessions_refresh_token_hash_key UNIQUE (refresh_token_hash);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_table_pkey PRIMARY KEY (id);

ALTER TABLE ONLY users
    ADD CONSTRAINT users_table_username_key UNIQUE (username);

ALTER TABLE ONLY volume_backup_policies
    ADD CONSTRAINT volume_backup_policies_pkey PRIMARY KEY (id);

ALTER TABLE ONLY volume_backups
    ADD CONSTRAINT volume_backups_pkey PRIMARY KEY (id);

ALTER TABLE ONLY vulnerability_ignores
    ADD CONSTRAINT vulnerability_ignores_pkey PRIMARY KEY (id);

ALTER TABLE ONLY vulnerability_reports
    ADD CONSTRAINT vulnerability_reports_pkey PRIMARY KEY (image_id);

ALTER TABLE ONLY vulnerability_risk_snapshots
    ADD CONSTRAINT vulnerability_risk_snapshots_pkey PRIMARY KEY (snapshot_date);

ALTER TABLE ONLY vulnerability_scan_items
    ADD CONSTRAINT vulnerability_scan_items_pkey PRIMARY KEY (id);

ALTER TABLE ONLY vulnerability_scans
    ADD CONSTRAINT vulnerability_scans_pkey PRIMARY KEY (id);

ALTER TABLE ONLY vulnerability_threat_intel
    ADD CONSTRAINT vulnerability_threat_intel_pkey PRIMARY KEY (cve_id);

ALTER TABLE ONLY webhooks
    ADD CONSTRAINT webhooks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY webhooks
    ADD CONSTRAINT webhooks_token_hash_key UNIQUE (token_hash);

CREATE INDEX idx_activities_environment_batch ON activities USING btree (environment_id, batch_id);

CREATE INDEX idx_activities_environment_started ON activities USING btree (environment_id, started_at);

CREATE INDEX idx_activities_environment_status_updated ON activities USING btree (environment_id, status, updated_at);

CREATE INDEX idx_activities_resource ON activities USING btree (resource_type, resource_id);

CREATE INDEX idx_activities_started_by_user_id ON activities USING btree (started_by_user_id);

CREATE INDEX idx_activities_type ON activities USING btree (type);

CREATE INDEX idx_activity_messages_activity_created ON activity_messages USING btree (activity_id, created_at);

CREATE INDEX idx_akp_key ON api_key_permissions USING btree (api_key_id);

CREATE UNIQUE INDEX idx_akp_uniq ON api_key_permissions USING btree (api_key_id, permission, COALESCE(environment_id, ''::text));

CREATE INDEX idx_api_keys_expires_at_not_null ON api_keys USING btree (expires_at) WHERE (expires_at IS NOT NULL);

CREATE INDEX idx_api_keys_key_hash ON api_keys USING btree (key_hash);

CREATE INDEX idx_api_keys_key_prefix ON api_keys USING btree (key_prefix);

CREATE INDEX idx_api_keys_user_managed_by_created_at ON api_keys USING btree (user_id, managed_by, created_at, id);

CREATE UNIQUE INDEX idx_apns_devices_recipient ON apns_devices USING btree (recipient_id);

CREATE INDEX idx_apns_devices_user ON apns_devices USING btree (user_id);

CREATE INDEX idx_apns_outbox_next_attempt ON apns_outbox USING btree (next_attempt_at);

CREATE INDEX idx_auth_transactions_expires_at ON auth_transactions USING btree (expires_at);

CREATE INDEX idx_auth_transactions_secret_hash ON auth_transactions USING btree (secret_hash);

CREATE INDEX idx_auth_transactions_status ON auth_transactions USING btree (status);

CREATE INDEX idx_auth_transactions_user_id ON auth_transactions USING btree (user_id);

CREATE INDEX idx_auto_update_resource_id ON auto_update_records USING btree (resource_id);

CREATE INDEX idx_auto_update_start_time ON auto_update_records USING btree (start_time);

CREATE INDEX idx_compose_templates_lookup_description ON compose_templates USING btree (is_remote, registry_id, description);

CREATE INDEX idx_compose_templates_lookup_name ON compose_templates USING btree (is_remote, registry_id, name);

CREATE INDEX idx_container_registries_enabled ON container_registries USING btree (enabled);

CREATE INDEX idx_environment_update_jobs_status ON environment_update_jobs USING btree (status);

CREATE INDEX idx_environments_access_token_not_null ON environments USING btree (access_token) WHERE (access_token IS NOT NULL);

CREATE INDEX idx_environments_api_url ON environments USING btree (api_url);

CREATE INDEX idx_environments_enabled ON environments USING btree (enabled);

CREATE INDEX idx_environments_enabled_true ON environments USING btree (id, name) WHERE (enabled = true);

CREATE INDEX idx_environments_hidden ON environments USING btree (hidden);

CREATE INDEX idx_environments_last_seen ON environments USING btree (last_seen);

CREATE INDEX idx_environments_name ON environments USING btree (name);

CREATE INDEX idx_environments_parent_environment_id ON environments USING btree (parent_environment_id);

CREATE INDEX idx_environments_parent_swarm_node ON environments USING btree (parent_environment_id, swarm_node_id);

CREATE INDEX idx_environments_status ON environments USING btree (status);

CREATE INDEX idx_environments_swarm_node_id ON environments USING btree (swarm_node_id);

CREATE UNIQUE INDEX idx_events_deduplication_key ON events USING btree (deduplication_key);

CREATE INDEX idx_events_environment_timestamp ON events USING btree (environment_id, "timestamp");

CREATE INDEX idx_events_resource_id ON events USING btree (resource_id);

CREATE INDEX idx_events_resource_type ON events USING btree (resource_type);

CREATE INDEX idx_events_severity ON events USING btree (severity);

CREATE INDEX idx_events_timestamp ON events USING btree ("timestamp");

CREATE INDEX idx_events_type ON events USING btree (type);

CREATE INDEX idx_events_user_id ON events USING btree (user_id);

CREATE INDEX idx_federated_credentials_enabled ON federated_credentials USING btree (enabled);

CREATE INDEX idx_federated_credentials_identity_user_id ON federated_credentials USING btree (identity_user_id);

CREATE INDEX idx_federated_credentials_issuer_url ON federated_credentials USING btree (issuer_url);

CREATE INDEX idx_federated_credentials_role_id ON federated_credentials USING btree (role_id);

CREATE INDEX idx_federated_token_replays_expires_at ON federated_token_replays USING btree (expires_at);

CREATE INDEX idx_federated_token_replays_issuer_url ON federated_token_replays USING btree (issuer_url);

CREATE INDEX idx_git_repositories_auth_type ON git_repositories USING btree (auth_type);

CREATE INDEX idx_git_repositories_enabled ON git_repositories USING btree (enabled);

CREATE INDEX idx_git_repositories_enabled_url ON git_repositories USING btree (enabled, url);

CREATE INDEX idx_git_repositories_name ON git_repositories USING btree (name);

CREATE INDEX idx_gitops_syncs_auto_sync ON gitops_syncs USING btree (auto_sync);

CREATE INDEX idx_gitops_syncs_auto_sync_true ON gitops_syncs USING btree (id, environment_id, sync_interval, last_sync_at) WHERE (auto_sync = true);

CREATE UNIQUE INDEX idx_gitops_syncs_backup_project ON gitops_syncs USING btree (project_id) WHERE (mode = 'backup'::text);

CREATE INDEX idx_gitops_syncs_environment_auto_sync ON gitops_syncs USING btree (environment_id, auto_sync);

CREATE INDEX idx_gitops_syncs_environment_id ON gitops_syncs USING btree (environment_id);

CREATE INDEX idx_gitops_syncs_environment_last_sync_status ON gitops_syncs USING btree (environment_id, last_sync_status);

CREATE INDEX idx_gitops_syncs_environment_project_id ON gitops_syncs USING btree (environment_id, project_id);

CREATE INDEX idx_gitops_syncs_environment_repository_id ON gitops_syncs USING btree (environment_id, repository_id);

CREATE INDEX idx_gitops_syncs_last_sync_commit ON gitops_syncs USING btree (last_sync_commit);

CREATE INDEX idx_gitops_syncs_project_id ON gitops_syncs USING btree (project_id);

CREATE INDEX idx_gitops_syncs_repository_id ON gitops_syncs USING btree (repository_id);

CREATE INDEX idx_gitops_syncs_sync_directory ON gitops_syncs USING btree (sync_directory);

CREATE INDEX idx_global_variable_environments_env ON global_variable_environments USING btree (environment_id);

CREATE INDEX idx_global_variables_key ON global_variables USING btree (key);

CREATE INDEX idx_image_builds_created_at ON image_builds USING btree (created_at);

CREATE INDEX idx_image_builds_env ON image_builds USING btree (environment_id);

CREATE INDEX idx_image_builds_environment_created_at ON image_builds USING btree (environment_id, created_at);

CREATE INDEX idx_image_builds_environment_status ON image_builds USING btree (environment_id, status);

CREATE INDEX idx_image_builds_status ON image_builds USING btree (status);

CREATE INDEX idx_image_patches_created_at ON image_patches USING btree (created_at);

CREATE INDEX idx_image_patches_env_image ON image_patches USING btree (environment_id, original_image_id);

CREATE INDEX idx_image_updates_check_time ON image_updates USING btree (check_time);

CREATE INDEX idx_image_updates_container_id ON image_updates USING btree (container_id);

CREATE INDEX idx_image_updates_has_update ON image_updates USING btree (has_update);

CREATE INDEX idx_image_updates_has_update_notification ON image_updates USING btree (has_update, notification_sent);

CREATE INDEX idx_image_updates_image_id ON image_updates USING btree (image_id);

CREATE INDEX idx_image_updates_notification_sent ON image_updates USING btree (notification_sent);

CREATE INDEX idx_image_updates_project_id ON image_updates USING btree (project_id);

CREATE INDEX idx_image_updates_repository_tag ON image_updates USING btree (repository, tag);

CREATE INDEX idx_images_repo ON images USING btree (repo);

CREATE INDEX idx_images_tag ON images USING btree (tag);

CREATE INDEX idx_notification_settings_enabled ON notification_settings USING btree (enabled);

CREATE INDEX idx_notification_settings_provider ON notification_settings USING btree (provider);

CREATE INDEX idx_orm_claim ON oidc_role_mappings USING btree (claim_value);

CREATE INDEX idx_passkey_ceremonies_expires_at ON passkey_ceremonies USING btree (expires_at);

CREATE INDEX idx_passkey_ceremonies_transaction_id ON passkey_ceremonies USING btree (auth_transaction_id);

CREATE INDEX idx_passkey_ceremonies_user_id ON passkey_ceremonies USING btree (user_id);

CREATE INDEX idx_passkey_recovery_codes_used_at ON passkey_recovery_codes USING btree (used_at);

CREATE INDEX idx_passkey_recovery_codes_user_id ON passkey_recovery_codes USING btree (user_id);

CREATE UNIQUE INDEX idx_passkeys_rp_credential ON passkeys USING btree (rp_id, credential_id);

CREATE INDEX idx_passkeys_user_id ON passkeys USING btree (user_id);

CREATE INDEX idx_project_tags_name ON project_tags USING btree (name);

CREATE INDEX idx_projects_dir_name_not_null ON projects USING btree (dir_name) WHERE (dir_name IS NOT NULL);

CREATE INDEX idx_projects_gitops_managed_by ON projects USING btree (gitops_managed_by);

CREATE INDEX idx_projects_is_archived ON projects USING btree (is_archived);

CREATE INDEX idx_projects_name ON projects USING btree (name);

CREATE UNIQUE INDEX idx_projects_path_unique ON projects USING btree (path);

CREATE INDEX idx_projects_status ON projects USING btree (status);

CREATE INDEX idx_settings_ispublic ON settings USING btree (ispublic);

CREATE INDEX idx_settings_key ON settings USING btree (key);

CREATE INDEX idx_system_backup_policies_s3_destination_id ON system_backup_policies USING btree (s3_destination_id);

CREATE INDEX idx_system_backup_runs_policy_id ON system_backup_runs USING btree (policy_id);

CREATE INDEX idx_system_backup_runs_s3_destination_id ON system_backup_runs USING btree (s3_destination_id);

CREATE INDEX idx_ura_env ON user_role_assignments USING btree (environment_id);

CREATE INDEX idx_ura_role ON user_role_assignments USING btree (role_id);

CREATE UNIQUE INDEX idx_ura_uniq ON user_role_assignments USING btree (user_id, role_id, COALESCE(environment_id, ''::text), source);

CREATE INDEX idx_ura_user ON user_role_assignments USING btree (user_id);

CREATE INDEX idx_user_sessions_expires_at ON user_sessions USING btree (expires_at);

CREATE INDEX idx_user_sessions_federated_credential_id ON user_sessions USING btree (federated_credential_id);

CREATE INDEX idx_user_sessions_user_id ON user_sessions USING btree (user_id);

CREATE INDEX idx_users_email ON users USING btree (email);

CREATE INDEX idx_users_oidc_subject_id ON users USING btree (oidc_subject_id);

CREATE UNIQUE INDEX idx_users_oidc_subject_id_unique ON users USING btree (oidc_subject_id) WHERE (oidc_subject_id IS NOT NULL);

CREATE INDEX idx_users_username ON users USING btree (username);

CREATE INDEX idx_volume_backup_policies_s3_destination_id ON volume_backup_policies USING btree (s3_destination_id);

CREATE INDEX idx_volume_backup_policies_volume_name ON volume_backup_policies USING btree (volume_name);

CREATE INDEX idx_volume_backups_policy_id ON volume_backups USING btree (policy_id);

CREATE INDEX idx_volume_backups_s3_destination_id ON volume_backups USING btree (s3_destination_id);

CREATE INDEX idx_volume_backups_volume_name_created_at ON volume_backups USING btree (volume_name, created_at);

CREATE INDEX idx_vulnerability_ignores_composite ON vulnerability_ignores USING btree (environment_id, image_id, vulnerability_id, pkg_name, installed_version);

CREATE INDEX idx_vulnerability_ignores_env_created_at ON vulnerability_ignores USING btree (environment_id, created_at);

CREATE INDEX idx_vulnerability_ignores_env_vulnerability_id ON vulnerability_ignores USING btree (environment_id, vulnerability_id);

CREATE INDEX idx_vulnerability_ignores_image ON vulnerability_ignores USING btree (image_id);

CREATE INDEX idx_vulnerability_scan_items_image_id ON vulnerability_scan_items USING btree (image_id);

CREATE INDEX idx_vulnerability_scan_items_severity ON vulnerability_scan_items USING btree (severity);

CREATE INDEX idx_vulnerability_scan_items_severity_rank_id ON vulnerability_scan_items USING btree (severity_rank, id);

CREATE INDEX idx_vulnerability_scan_items_vulnerability_id ON vulnerability_scan_items USING btree (vulnerability_id);

CREATE INDEX idx_vulnerability_scans_scan_time ON vulnerability_scans USING btree (scan_time);

CREATE INDEX idx_vulnerability_scans_status_total_count ON vulnerability_scans USING btree (status, total_count);

CREATE INDEX idx_vulnerability_threat_intel_known_exploited ON vulnerability_threat_intel USING btree (known_exploited);

CREATE INDEX idx_webhooks_environment_id ON webhooks USING btree (environment_id);

CREATE INDEX idx_webhooks_token_prefix ON webhooks USING btree (token_prefix);

ALTER TABLE ONLY activity_messages
    ADD CONSTRAINT activity_messages_activity_id_fkey FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE;

ALTER TABLE ONLY api_key_permissions
    ADD CONSTRAINT api_key_permissions_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE;

ALTER TABLE ONLY api_key_permissions
    ADD CONSTRAINT api_key_permissions_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY api_keys
    ADD CONSTRAINT api_keys_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY apns_devices
    ADD CONSTRAINT apns_devices_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY auth_transactions
    ADD CONSTRAINT auth_transactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY compose_templates
    ADD CONSTRAINT compose_templates_registry_id_fkey FOREIGN KEY (registry_id) REFERENCES template_registries(id) ON DELETE SET NULL;

ALTER TABLE ONLY environments
    ADD CONSTRAINT environments_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;

ALTER TABLE ONLY federated_credentials
    ADD CONSTRAINT federated_credentials_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL;

ALTER TABLE ONLY federated_credentials
    ADD CONSTRAINT federated_credentials_identity_user_id_fkey FOREIGN KEY (identity_user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY federated_credentials
    ADD CONSTRAINT federated_credentials_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id);

ALTER TABLE ONLY gitops_syncs
    ADD CONSTRAINT gitops_syncs_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY gitops_syncs
    ADD CONSTRAINT gitops_syncs_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

ALTER TABLE ONLY gitops_syncs
    ADD CONSTRAINT gitops_syncs_repository_id_fkey FOREIGN KEY (repository_id) REFERENCES git_repositories(id) ON DELETE CASCADE;

ALTER TABLE ONLY global_variable_environments
    ADD CONSTRAINT global_variable_environments_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY global_variable_environments
    ADD CONSTRAINT global_variable_environments_global_variable_id_fkey FOREIGN KEY (global_variable_id) REFERENCES global_variables(id) ON DELETE CASCADE;

ALTER TABLE ONLY oidc_role_mappings
    ADD CONSTRAINT oidc_role_mappings_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY oidc_role_mappings
    ADD CONSTRAINT oidc_role_mappings_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE;

ALTER TABLE ONLY passkey_ceremonies
    ADD CONSTRAINT passkey_ceremonies_auth_transaction_id_fkey FOREIGN KEY (auth_transaction_id) REFERENCES auth_transactions(id) ON DELETE CASCADE;

ALTER TABLE ONLY passkey_ceremonies
    ADD CONSTRAINT passkey_ceremonies_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY passkey_recovery_codes
    ADD CONSTRAINT passkey_recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY passkeys
    ADD CONSTRAINT passkeys_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY project_tags
    ADD CONSTRAINT project_tags_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY user_avatars
    ADD CONSTRAINT user_avatars_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY user_role_assignments
    ADD CONSTRAINT user_role_assignments_environment_id_fkey FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

ALTER TABLE ONLY user_role_assignments
    ADD CONSTRAINT user_role_assignments_role_id_fkey FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE;

ALTER TABLE ONLY user_role_assignments
    ADD CONSTRAINT user_role_assignments_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY user_sessions
    ADD CONSTRAINT user_sessions_federated_credential_id_fkey FOREIGN KEY (federated_credential_id) REFERENCES federated_credentials(id) ON DELETE SET NULL;

ALTER TABLE ONLY user_sessions
    ADD CONSTRAINT user_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE ONLY vulnerability_scan_items
    ADD CONSTRAINT vulnerability_scan_items_image_id_fkey FOREIGN KEY (image_id) REFERENCES vulnerability_scans(id) ON DELETE CASCADE;

INSERT INTO settings (key, value) VALUES
    ('vulnerabilityScanEnabled', 'false'),
    ('vulnerabilityScanInterval', '0 0 0 * * *'),
    ('autoUpdateExcludedContainers', '');

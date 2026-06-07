-- Recreate containers table (for rollback only)
CREATE TABLE IF NOT EXISTS containers (
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
    project_id TEXT,
    started_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- Recreate volumes table (for rollback only)
CREATE TABLE IF NOT EXISTS volumes (
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

-- Recreate networks table (for rollback only)
CREATE TABLE IF NOT EXISTS networks (
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

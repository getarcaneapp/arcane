set working-directory := '..'

[default]
[private]
list:
    @{{ just_executable() }} --justfile {{ justfile() }} --list dev

# Frontend dev server on port 3000
frontend:
    vp -C frontend run dev

# Backend with hot reload on port 3552
backend:
    cd backend && air

# Local edge agent against a running manager (requires AGENT_TOKEN)
[script('bash')]
agent:
    set -euo pipefail

    if [ -z "${AGENT_TOKEN:-}" ]; then
        echo "AGENT_TOKEN is required. Run: AGENT_TOKEN=<edge-environment-token> just dev agent"
        exit 1
    fi

    port="${PORT:-3553}"
    app_url="${APP_URL:-http://localhost:${port}}"
    manager_api_url="${MANAGER_API_URL:-https://localhost:3552}"
    edge_mtls_assets_dir="${EDGE_MTLS_ASSETS_DIR:-./.tmp/edge-test-agent/edge-mtls-agent}"
    edge_mtls_ca_file="${EDGE_MTLS_CA_FILE:-./backend/local-manager.crt}"
    database_url="${DATABASE_URL:-file:./.tmp/edge-test-agent/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate}"
    projects_directory="${PROJECTS_DIRECTORY:-./.tmp/edge-test-agent/projects}"
    git_work_dir="${GIT_WORK_DIR:-./.tmp/edge-test-agent/git}"
    jwt_secret="${JWT_SECRET:-local-edge-test-jwt-secret-please-change}"
    encryption_key="${ENCRYPTION_KEY:-local-edge-test-encryption-key-32}"

    mkdir -p "${projects_directory}" "${git_work_dir}" "${edge_mtls_assets_dir}"

    PORT="${port}" \
    APP_URL="${app_url}" \
    EDGE_AGENT=true \
    EDGE_TRANSPORT=poll \
    EDGE_MTLS_MODE=required \
    EDGE_MTLS_ASSETS_DIR="${edge_mtls_assets_dir}" \
    EDGE_MTLS_CA_FILE="${edge_mtls_ca_file}" \
    AGENT_TOKEN="${AGENT_TOKEN}" \
    MANAGER_API_URL="${manager_api_url}" \
    DATABASE_URL="${database_url}" \
    PROJECTS_DIRECTORY="${projects_directory}" \
    GIT_WORK_DIR="${git_work_dir}" \
    JWT_SECRET="${jwt_secret}" \
    ENCRYPTION_KEY="${encryption_key}" \
    go run ./backend/cmd

# Backend and frontend together
[script('bash')]
all:
    set -euo pipefail

    shutdown() {
        status=$?
        trap - EXIT
        trap '' INT TERM
        kill -TERM "$backend_pid" "$frontend_pid" 2>/dev/null || true
        wait "$backend_pid" "$frontend_pid" 2>/dev/null || true
        exit "$status"
    }

    (cd backend && exec air) &
    backend_pid=$!
    vp -C frontend run dev &
    frontend_pid=$!
    trap shutdown EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    wait "$frontend_pid"

# Rebuild the Docker dev environment
docker:
    ./scripts/development/dev.sh rebuild

# Follow Docker dev environment logs
logs:
    ./scripts/development/dev.sh logs

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

# Local Arcane agent against a running manager (requires ARCANE_AGENT_TOKEN)
[script('bash')]
agent:
    set -euo pipefail

    if [ -z "${ARCANE_AGENT_TOKEN:-}" ]; then
        echo "ARCANE_AGENT_TOKEN is required. Run: ARCANE_AGENT_TOKEN=<agent-environment-token> just dev agent"
        exit 1
    fi

    manager_api_url="${ARCANE_MANAGER_API_URL:-https://localhost:3552}"
    agent_mtls_assets_dir="${ARCANE_AGENT_MTLS_ASSETS_DIR:-./.tmp/test-agent/agent-mtls-client}"
    agent_mtls_ca_file="${ARCANE_AGENT_MTLS_CA_FILE:-./backend/local-manager.crt}"
    database_url="${ARCANE_DATABASE_URL:-file:./.tmp/test-agent/arcane.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate}"
    projects_directory="${ARCANE_PROJECTS_DIRECTORY:-./.tmp/test-agent/projects}"
    git_work_dir="${ARCANE_GIT_WORK_DIR:-./.tmp/test-agent/git}"
    encryption_key="${ARCANE_ENCRYPTION_KEY:-local-agent-test-encryption-key-32}"

    mkdir -p "${projects_directory}" "${git_work_dir}" "${agent_mtls_assets_dir}"

    ARCANE_AGENT=true \
    ARCANE_AGENT_MTLS_MODE=required \
    ARCANE_AGENT_MTLS_ASSETS_DIR="${agent_mtls_assets_dir}" \
    ARCANE_AGENT_MTLS_CA_FILE="${agent_mtls_ca_file}" \
    ARCANE_AGENT_TOKEN="${ARCANE_AGENT_TOKEN}" \
    ARCANE_MANAGER_API_URL="${manager_api_url}" \
    ARCANE_DATABASE_URL="${database_url}" \
    ARCANE_PROJECTS_DIRECTORY="${projects_directory}" \
    ARCANE_GIT_WORK_DIR="${git_work_dir}" \
    ARCANE_ENCRYPTION_KEY="${encryption_key}" \
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

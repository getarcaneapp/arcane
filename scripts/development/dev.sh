#!/bin/bash

# Arcane Development Environment Manager
# This script helps manage the Docker-based development environment with hot reloading

set -euo pipefail

# Configuration
readonly COMPOSE_FILE="docker/compose.dev.yaml"
readonly PROJECT_NAME="arcane-dev"
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

install_docker() {
    local platform arch
    platform="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"
    
    log_info "Installing Docker using download script..."
    if ! "${SCRIPT_DIR}/download-docker.sh" "$platform" "$arch" "28.4.0"; then
        log_error "Failed to install Docker"
        return 1
    fi
    
    # Add to PATH for current session
    export PATH="${PROJECT_ROOT}/dist:$PATH"
    log_success "Docker installed successfully"
    return 0
}

install_compose() {
    local dest="${PROJECT_ROOT}/dist/docker-compose"
    
    log_info "Installing Docker Compose using download script..."
    if ! "${SCRIPT_DIR}/download-compose.sh" "2.39.2" "$dest"; then
        log_error "Failed to install Docker Compose"
        return 1
    fi
    
    # Add to PATH for current session
    export PATH="${PROJECT_ROOT}/dist:$PATH"
    log_success "Docker Compose installed successfully"
    return 0
}

offer_installation() {
    local missing_tools=()
    local install_docker=false
    local install_compose=false
    
    # Check what's missing
    if ! command -v docker &> /dev/null; then
        missing_tools+=("Docker")
        install_docker=true
    fi
    
    if ! docker compose version &> /dev/null 2>&1; then
        missing_tools+=("Docker Compose")
        install_compose=true
    fi
    
    if [[ ${#missing_tools[@]} -eq 0 ]]; then
        return 0
    fi
    
    log_warning "Missing required tools: ${missing_tools[*]}"
    echo
    log_info "We can automatically install these tools for you using the project's download scripts."
    log_info "The tools will be installed locally in the project directory (${PROJECT_ROOT}/dist/)"
    echo
    
    read -p "Would you like to install the missing tools automatically? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Installation cancelled."
        log_info "Please install Docker and Docker Compose manually, then run this script again."
        return 1
    fi
    
    # Install missing tools
    if [[ $install_docker == true ]]; then
        if ! install_docker; then
            return 1
        fi
    fi
    
    if [[ $install_compose == true ]]; then
        if ! install_compose; then
            return 1
        fi
    fi
    
    log_success "All required tools installed successfully!"
    echo
    return 0
}

check_requirements() {
    local docker_available=true
    local daemon_running=true
    local compose_available=true
    
    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        docker_available=false
    fi
    
    # Check if Docker daemon is running (only if Docker is available)
    if [[ $docker_available == true ]] && ! docker info &> /dev/null; then
        daemon_running=false
    fi
    
    # Check if Docker Compose is available
    if ! docker compose version &> /dev/null; then
        compose_available=false
    fi
    
    # If Docker or Compose are missing, offer installation
    if [[ $docker_available == false ]] || [[ $compose_available == false ]]; then
        if ! offer_installation; then
            exit 1
        fi
        
        # Re-check after installation
        if ! command -v docker &> /dev/null; then
            log_error "Docker installation failed or is not in PATH"
            exit 1
        fi
        
        if ! docker compose version &> /dev/null; then
            log_error "Docker Compose installation failed or is not in PATH"
            exit 1
        fi
    fi
    
    # Check Docker daemon (after ensuring Docker is installed)
    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running"
        log_error "Please start Docker Desktop or the Docker daemon and try again"
        exit 1
    fi
    
    log_info "All requirements satisfied ✓"
}

ensure_project_root() {
    if [[ ! -f "${PROJECT_ROOT}/${COMPOSE_FILE}" ]]; then
        log_error "Could not find ${COMPOSE_FILE} in project root"
        log_error "Please run this script from the project root directory or ensure the compose file exists"
        exit 1
    fi
    
    # Change to project root to ensure relative paths work correctly
    cd "${PROJECT_ROOT}"
}

show_status() {
    log_info "Development environment status:"
    docker compose -f $COMPOSE_FILE -p $PROJECT_NAME ps
}

show_env_config() {
    local service="${1:-backend}"
    
    log_info "Current environment configuration:"
    
    # Check if the specified service container is running
    if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" ps "$service" | grep -q "Up"; then
        log_info "Environment variables from running $service container:"
        echo "----------------------------------------"
        if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec "$service" env | grep -E "^[A-Z_]+" | sort; then
            echo "----------------------------------------"
        else
            log_error "Failed to retrieve environment variables from $service container"
            return 1
        fi
    else
        log_warning "$service container is not running."
        if [[ -f ".env" ]]; then
            log_info "Environment variables from .env file (container not started):"
            echo "----------------------------------------"
            grep -E "^[A-Z_]+" .env | sort
            echo "----------------------------------------"
        else
            log_warning "No .env file found (not needed for development - values are in docker/compose.dev.yaml)"
        fi
    fi
}

show_logs() {
    local service="${1:-}"
    
    if [[ -z "$service" ]]; then
        log_info "Showing logs for all services..."
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" logs -f
    else
        # Validate service name
        if [[ ! "$service" =~ ^(frontend|backend|arcane-agent|agent)$ ]]; then
            log_error "Invalid service name: $service"
            log_error "Valid services: frontend, backend, agent"
            exit 1
        fi
        
        # Normalize agent service name
        if [[ "$service" == "agent" ]]; then
            service="arcane-agent"
        fi
        
        log_info "Showing logs for service: $service"
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" logs -f "$service"
    fi
}

start_dev() {
    log_info "Starting Arcane development environment..."
    
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" up -d --build; then
        log_error "Failed to start development environment"
        exit 1
    fi
    
    log_success "Development environment started!"
    log_info "Frontend: http://localhost:3000"
    log_info "Backend:  http://localhost:3552"
    log_info ""
    log_info "Use './scripts/development/dev.sh logs' to view logs"
    log_info "Use './scripts/development/dev.sh logs frontend' or './scripts/development/dev.sh logs backend' for specific service logs"
}

stop_dev() {
    log_info "Stopping Arcane development environment..."
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down; then
        log_error "Failed to stop development environment"
        exit 1
    fi
    log_success "Development environment stopped!"
}

restart_dev() {
    log_info "Restarting Arcane development environment..."
    
    # Stop containers gracefully to allow Air to clean up properly
    log_info "Stopping containers gracefully..."
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down; then
        log_error "Failed to stop development environment"
        exit 1
    fi
    
    # Wait a moment for file locks to be released
    log_info "Waiting for file locks to be released..."
    sleep 2
    
    # Start containers with fresh environment
    log_info "Starting containers with updated environment..."
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" up -d; then
        log_error "Failed to start development environment"
        exit 1
    fi
    
    log_success "Development environment restarted with updated configuration!"
    log_info "Frontend: http://localhost:3000"
    log_info "Backend:  http://localhost:3552"
}

show_status() {
    log_info "Complete development environment status:"
    echo "========================================"
    
    # Docker Environment
    echo
    log_info "🐳 Docker Environment:"
    echo "----------------------------------------"
    
    # Show running containers
    local containers=$(docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" ps --format "table" 2>/dev/null || echo "No containers found")
    echo "Containers:"
    echo "$containers"
    echo
    
    # Show volumes
    echo "Volumes:"
    docker volume ls --filter "name=arcane" --format "table {{.Name}}\t{{.Size}}" 2>/dev/null || echo "No volumes found"
    echo
    
    # Show networks
    echo "Networks:"
    docker network ls --filter "name=arcane" --format "table {{.Name}}\t{{.Driver}}" 2>/dev/null || echo "No networks found"
    
    # Build Performance & Caches
    echo
    log_info "🔧 Build Performance & Caches:"
    echo "----------------------------------------"
    
    # Show build cache status
    if docker volume ls | grep -q "arcane-go-build-cache"; then
        local build_cache_size=$(docker run --rm -v arcane-go-build-cache:/cache alpine du -sh /cache 2>/dev/null | cut -f1 || echo "Unknown")
        echo "Go build cache: ${build_cache_size}"
    else
        echo "Go build cache: Not created"
    fi
    
    if docker volume ls | grep -q "arcane-go-mod-cache"; then
        local mod_cache_size=$(docker run --rm -v arcane-go-mod-cache:/cache alpine du -sh /cache 2>/dev/null | cut -f1 || echo "Unknown")
        echo "Go module cache: ${mod_cache_size}"
    else
        echo "Go module cache: Not created"
    fi
    
    # Show local build artifacts
    if [[ -f "${PROJECT_ROOT}/backend/.bin/arcane" ]]; then
        local binary_size=$(du -sh "${PROJECT_ROOT}/backend/.bin/arcane" | cut -f1)
        local binary_date=$(stat -c %y "${PROJECT_ROOT}/backend/.bin/arcane" 2>/dev/null || stat -f %Sm "${PROJECT_ROOT}/backend/.bin/arcane")
        echo "Backend binary: ${binary_size} (${binary_date})"
    else
        echo "Backend binary: Not built"
    fi
    
    if [[ -d "${PROJECT_ROOT}/backend/.bin" ]]; then
        local build_dir_size=$(du -sh "${PROJECT_ROOT}/backend/.bin" | cut -f1)
        echo "Build directory: ${build_dir_size}"
    else
        echo "Build directory: Not found"
    fi
    
    echo "========================================"
}

clean_containers_only() {
    log_warning "This will stop and remove all development containers."
    log_info "Volumes, networks, and data will be preserved."
    read -p "Continue? (y/N): " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        log_info "Stopping and removing containers..."
        if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down --remove-orphans; then
            log_success "Containers cleaned successfully!"
            log_info "To restart: $0 start"
        else
            log_error "Failed to clean containers"
            return 1
        fi
    else
        log_info "Container cleanup cancelled."
    fi
}

clean_environment_preserve_data() {
    log_warning "This will remove containers and networks."
    log_info "All volumes and application data will be preserved."
    read -p "Continue? (y/N): " -n 1 -r
    echo
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        log_info "Resetting Docker environment..."
        if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down --remove-orphans; then
            # Remove the specific network
            docker network rm arcane-dev-network 2>/dev/null || true
            log_success "Docker environment reset successfully!"
            log_info "All data and caches preserved."
            log_info "To restart: $0 start"
        else
            log_error "Failed to reset Docker environment"
            return 1
        fi
    else
        log_info "Reset cancelled."
    fi
}

clean_nuclear() {
    log_error "⚠️  NUCLEAR CLEANUP - ALL DATA WILL BE LOST ⚠️"
    log_warning "This will remove:"
    echo "  • All containers and images"
    echo "  • All networks"
    echo "  • All volumes (including databases and uploads)"
    echo "  • All build caches"
    echo "  • Local build artifacts"
    echo
    log_warning "This action cannot be undone!"
    read -p "Type 'DESTROY' to confirm nuclear cleanup: " -r
    echo
    
    if [[ "$REPLY" == "DESTROY" ]]; then
        log_info "Performing nuclear cleanup..."
        
        # Stop everything
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down -v --remove-orphans || true
        
        # Remove all project-related volumes
        docker volume ls --filter "name=arcane" --format "{{.Name}}" | xargs -r docker volume rm 2>/dev/null || true
        
        # Remove networks
        docker network rm arcane-dev-network 2>/dev/null || true
        
        # Clean local artifacts
        rm -rf "${PROJECT_ROOT}/backend/.bin" 2>/dev/null || true
        
        # System prune
        docker system prune -f --volumes || log_warning "System prune failed, but cleanup completed"
        
        log_success "Nuclear cleanup completed!"
        log_info "Environment completely reset. To restart: $0 start"
    else
        log_info "Nuclear cleanup cancelled."
    fi
}


clean_dev() {
    echo
    log_info "Development Environment Cleanup & Troubleshooting:"
    echo
    echo "📊 Status & Information:"
    echo "1) Show environment status"
    echo "   └─ View containers, networks, volumes, and build caches"
    echo
    echo "🔧 Build Performance Issues:"
    echo "2) Fix slow builds - clean build cache"
    echo "   └─ Removes compiled Go packages, keeps modules"
    echo "   └─ Recovery: ~30-60 seconds | Use for: Builds taking too long"
    echo
    echo "3) Fix dependency issues - clean module cache"
    echo "   └─ Removes downloaded modules, keeps compiled packages"
    echo "   └─ Recovery: ~2-3 minutes | Use for: After go.mod changes"
    echo
    echo "4) Quick build fix - clean local binary"
    echo "   └─ Removes backend binary only, keeps all caches"
    echo "   └─ Recovery: ~2-5 seconds | Use for: Binary won't start"
    echo
    echo "🐳 Docker Environment Issues:"
    echo "5) Restart containers (soft reset)"
    echo "   └─ Stops and starts containers, preserves all data"
    echo "   └─ Recovery: ~30 seconds | Use for: Container/port issues"
    echo
    echo "6) Reset Docker environment (preserve data)"
    echo "   └─ Removes containers and networks, keeps all data"
    echo "   └─ Recovery: ~1 minute | Use for: Network/environment issues"
    echo
    echo "🧹 Complete Cleanup:"
    echo "7) Clean all build caches (preserve Docker data)"
    echo "   └─ Removes all build artifacts, keeps application data"
    echo "   └─ Recovery: ~3-5 minutes | Use for: All build issues"
    echo
    echo "8) Nuclear reset - destroy everything"
    echo "   └─ Removes ALL containers, networks, volumes, and data"
    echo "   └─ Recovery: ~5-10 minutes | Use for: Complete corruption"
    echo
    echo "🚀 Maintenance:"
    echo "9) Warm up caches (after cleaning)"
    echo "   └─ Pre-populates caches for faster subsequent builds"
    echo "   └─ Recovery: ~1-2 minutes | Use for: After cache cleaning"
    echo
    echo "10) Optimize caches (weekly maintenance)"
    echo "    └─ Cleans unused modules and optimizes build performance"
    echo "    └─ Recovery: ~1-2 minutes | Use for: Regular maintenance"
    echo
    echo "0) Exit"
    echo
    
    read -p "Select option (0-10) [default: 0]: " -r
    echo
    
    if [[ -z "$REPLY" ]]; then
        REPLY="0"
    fi
    
    case $REPLY in
    1)
        show_status
        ;;
    2)
        clean_cache_type "build"
        ;;
    3)
        clean_cache_type "modules"
        ;;
    4)
        clean_cache_type "binary"
        ;;
    5)
        clean_containers_only
        ;;
    6)
        clean_environment_preserve_data
        ;;
    7)
        clean_cache_type "all"
        ;;
    8)
        clean_nuclear
        ;;
    9)
        warm_cache
        ;;
    10)
        optimize_cache
        ;;
    0|"")
        return 0
        ;;
    *)
        log_error "Invalid option: $REPLY"
        ;;
    esac
    
    echo
    read -p "Press Enter to continue..."
}

rebuild_dev() {
    log_info "Rebuilding development environment..."
    
    # Stop containers
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" down; then
        log_error "Failed to stop containers"
        exit 1
    fi
    
    # Clean up any leftover build artifacts
    log_info "Cleaning up build artifacts..."
    if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend rm -rf .bin 2>/dev/null; then
        log_info "Cleaned backend build directory"
    fi
    
    # Rebuild with no cache
    log_info "Rebuilding containers from scratch..."
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" build --no-cache; then
        log_error "Failed to rebuild containers"
        exit 1
    fi
    
    # Start fresh
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" up -d; then
        log_error "Failed to start containers"
        exit 1
    fi
    
    log_success "Development environment rebuilt and started!"
    log_info "Frontend: http://localhost:3000"
    log_info "Backend:  http://localhost:3552"
}

shell_into() {
    local service="${1:-}"
    
    if [[ -z "$service" ]]; then
        log_error "Please specify a service: frontend, backend, or agent"
        exit 1
    fi
    
    # Validate service name
    if [[ ! "$service" =~ ^(frontend|backend|arcane-agent|agent)$ ]]; then
        log_error "Invalid service name: $service"
        log_error "Valid services: frontend, backend, agent"
        exit 1
    fi
    
    # Normalize agent service name
    if [[ "$service" == "agent" ]]; then
        service="arcane-agent"
    fi
    
    log_info "Opening shell in $service container..."
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec "$service" /bin/sh; then
        log_error "Failed to open shell in $service container"
        log_error "Make sure the container is running: ./scripts/development/dev.sh status"
        exit 1
    fi
}

# Cache Management Functions

clean_cache_type() {
    local cache_type="${1:-all}"
    
    case "$cache_type" in
    "build")
        log_warning "Cleaning Go build cache..."
        if docker volume ls | grep -q "arcane-go-build-cache"; then
            # Stop containers that might be using the volume
            docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" stop backend arcane-agent 2>/dev/null || true
            docker volume rm arcane-go-build-cache || true
            log_success "Build cache cleaned"
        else
            log_info "Build cache doesn't exist"
        fi
        ;;
    "modules")
        log_warning "Cleaning Go module cache..."
        if docker volume ls | grep -q "arcane-go-mod-cache"; then
            # Stop containers that might be using the volume
            docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" stop backend arcane-agent 2>/dev/null || true
            docker volume rm arcane-go-mod-cache || true
            log_success "Module cache cleaned"
        else
            log_info "Module cache doesn't exist"
        fi
        ;;
    "binary")
        log_warning "Cleaning backend binary..."
        if [[ -f "${PROJECT_ROOT}/backend/.bin/arcane" ]]; then
            rm -f "${PROJECT_ROOT}/backend/.bin/arcane"
            log_success "Backend binary cleaned"
        else
            log_info "Backend binary doesn't exist"
        fi
        ;;
    "all")
        log_warning "This will clean all Go caches and force a complete rebuild."
        read -p "Are you sure? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            clean_cache_type "build"
            clean_cache_type "modules"
            clean_cache_type "binary"
            log_success "All caches cleaned"
        else
            log_info "Cache cleaning cancelled"
        fi
        ;;
    *)
        log_error "Invalid cache type: $cache_type"
        log_error "Valid types: build, modules, binary, all"
        exit 1
        ;;
    esac
}

warm_cache() {
    log_info "Warming up Go caches..."
    
    # Ensure containers are running
    if ! docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" ps backend | grep -q "Up"; then
        log_info "Starting backend container to warm cache..."
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" up -d backend
        sleep 5
    fi
    
    # Trigger a build to warm the cache
    log_info "Triggering initial build to populate cache..."
    docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend go mod download
    docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend go build -tags 'exclude_frontend' -o /tmp/warmup ./cmd
    
    log_success "Cache warmed up successfully"
}

optimize_cache() {
    log_info "Optimizing Go caches..."
    
    if docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" ps backend | grep -q "Up"; then
        # Clean module cache of unused modules
        log_info "Cleaning unused modules..."
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend go mod tidy
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend go clean -modcache
        
        # Rebuild module cache
        docker compose -f "${COMPOSE_FILE}" -p "${PROJECT_NAME}" exec backend go mod download
        
        log_success "Cache optimized"
    else
        log_warning "Backend container is not running. Start it first with: $0 start"
    fi
}


show_help() {
    echo "Arcane Development Environment Manager"
    echo
    echo "Usage: $0 [COMMAND]"
    echo
    echo "Commands:"
    echo "  start     Start the development environment"
    echo "  stop      Stop the development environment"
    echo "  restart   Restart the development environment"
    echo "  status    Show status of all services"
    echo "  env       Show current environment configuration (optionally specify service: backend, frontend)"
    echo "  logs      Show logs (optionally specify service: frontend, backend, agent)"
    echo "  clean     Unified cleanup & troubleshooting menu"
    echo "  rebuild   Rebuild and restart the development environment"
    echo "  shell     Open shell in a service container (specify: frontend, backend, or agent)"
    echo "  help      Show this help message"
    echo
    echo "Features:"
    echo "  • Automatic Docker/Compose installation if missing (using project scripts)"
    echo "  • Hot reload for both frontend (Vite) and backend (Air)"
    echo "  • Persistent Go build caches for faster incremental builds"
    echo "  • Interactive cache management with clear guidance"
    echo "  • Interactive log viewing with service selection"
    echo "  • Automatic project root detection"
    echo
    echo "Examples:"
    echo "  $0 start"
    echo "  $0 clean"
    echo "  $0 env backend"
    echo "  $0 logs backend"
    echo "  $0 shell frontend"
    echo
    echo "Note: If Docker or Docker Compose are not installed, you'll be prompted"
    echo "      to install them automatically using the project's download scripts."
}

# Main script logic
main() {
    # Check requirements and ensure we're in the right directory
    check_requirements
    ensure_project_root
    
    local command="${1:-help}"
    
    case "$command" in
    start)
        start_dev
        ;;
    stop)
        stop_dev
        ;;
    restart)
        restart_dev
        ;;
    status)
        show_status
        ;;
    env)
        show_env_config "${2:-}"
        ;;
    logs)
        show_logs "${2:-}"
        ;;
    clean)
        clean_dev
        ;;
    rebuild)
        rebuild_dev
        ;;
    shell)
        shell_into "${2:-}"
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        log_error "Unknown command: $command"
        echo
        show_help
        exit 1
        ;;
    esac
}

# Run main function with all arguments
main "$@"
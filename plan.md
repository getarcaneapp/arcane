Plan: GitOps Sync with Reusable Repositories
Implement GitOps-style repo syncing with a two-model architecture: reusable GitRepository (credentials) and GitOpsSync (sync configuration). Users define repositories once, then create multiple sync configurations referencing different branches/paths from the same repo. Follows container registry credential reuse pattern.

Steps
Create types in types package – Add types/gitops/gitops.go defining GitRepository (URL, authType, username, encrypted token/sshKey), GitOpsSync (repositoryID FK, branch, composePath, projectID, autoSync, syncInterval, lastSync\* fields), plus CreateRepositoryRequest, UpdateRepositoryRequest, CreateSyncRequest, UpdateSyncRequest, SyncResult, FileTreeNode, BrowseRequest, BrowseResponse structs with OpenAPI tags following types/containerregistry/containerregistry.go pattern.

Create database models & migrations – Add backend/internal/models/git_repository.go with encrypted token/sshKey fields and backend/internal/models/gitops_sync.go with RepositoryID and ProjectID foreign keys. Create up/down migrations for both postgres and sqlite under migrations following BaseModel pattern.

Create git utility package – Add backend/internal/utils/git/git.go using go-git/go-git/v5 library with Clone, Pull, BrowseTree methods supporting HTTP (basic auth, token) and SSH key auth with proper path traversal prevention and temporary directory management.

Create service layer – Add backend/internal/services/git_repository_service.go with CRUD, GetDecryptedToken, GetDecryptedSSHKey, GetEnabledRepositories, TestConnection methods using utils.Encrypt/utils.Decrypt pattern from encryption.go. Add backend/internal/services/gitops_sync_service.go with CRUD, PerformSync (clones repo, copies compose file to project via ProjectService), GetSyncStatus, SyncAllEnabled, BrowseFiles methods with GORM preloading for Repository relationship.

Create Huma handlers – Add backend/internal/huma/handlers/git-repositories.go and backend/internal/huma/handlers/gitops-syncs.go following backend/internal/huma/handlers/container-registries.go pattern with endpoints: GET/POST/PUT/DELETE /git-repositories, GET/POST/PUT/DELETE /gitops-syncs, POST /gitops-syncs/{id}/sync, GET /gitops-syncs/{id}/files, GET /gitops-syncs/{id}/status.

Create sync job – Add backend/internal/job/gitops_sync_job.go following backend/internal/job/container_update_job.go pattern with configurable interval executing SyncAllEnabled for enabled syncs with autoSync.

Wire up bootstrap – Register GitRepositoryService and GitOpsSyncService in services_bootstrap.go, register job in jobs_bootstrap.go, register handlers in backend/internal/bootstrap/api_bootstrap.go.

Create frontend types & services – Add frontend/src/lib/types/gitops.type.ts with GitRepository and GitOpsSync interfaces, and frontend/src/lib/services/git-repository-service.ts and frontend/src/lib/services/gitops-sync-service.ts following container-registry-service.ts pattern.

Create frontend UI pages – Add routes at frontend/src/routes/(app)/customize/git-repositories//customize/git-repositories/) and frontend/src/routes/(app)/customize/gitops-syncs//customize/gitops-syncs/) with +page.ts loaders, +page.svelte main pages showing status cards (next sync time, active syncs), repository-table.svelte and sync-table.svelte components with status indicators, and file-browser.svelte tree component for browsing synced files.

Create sheet components – Add frontend/src/lib/components/sheets/git-repository-sheet.svelte with form for name, URL, auth type toggle (HTTP/SSH/None), credential fields, and frontend/src/lib/components/sheets/gitops-sync-sheet.svelte with repository dropdown (select from existing repos), branch, composePath, project link, autoSync toggle, sync interval input.

The frontend components should be on the same page, but use tabs to switch between the two. All routes should also work on remote environments as well for the api

Update navigation & i18n – Add Git Repositories and GitOps Syncs links to customize section in constants.ts navigation config and add i18n keys to en.json.

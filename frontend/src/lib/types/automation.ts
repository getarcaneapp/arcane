// --- Auto-update ---

export type AutoUpdateResourceType = 'image' | 'container' | 'project';

export interface AutoUpdateCheck {
	/** Scopes `resourceIds`. Matches the backend's `updater.Options.Type` (singular). */
	type?: AutoUpdateResourceType;
	resourceIds?: string[];
	forceUpdate?: boolean;
	dryRun?: boolean;
}

export interface AutoUpdateResult {
	checked: number;
	updated: number;
	restarted?: number;
	skipped: number;
	failed: number;
	items: AutoUpdateResourceResult[];
	duration: string;
	activityId?: string;
}

export interface AutoUpdateResourceResult {
	resourceId: string;
	resourceName: string;
	resourceType: AutoUpdateResourceType;
	status: 'checked' | 'up_to_date' | 'update_available' | 'updated' | 'restarted' | 'failed' | 'skipped';
	updateAvailable: boolean;
	updateApplied: boolean;
	oldImages?: Record<string, string>;
	newImages?: Record<string, string>;
	error?: string;
	details?: Record<string, any>;
}

// --- Pruning ---

export type PruneContainerMode = 'none' | 'stopped' | 'olderThan';
export type PruneImageMode = 'none' | 'dangling' | 'all' | 'olderThan';
export type PruneVolumeMode = 'none' | 'anonymous' | 'all';
export type PruneNetworkMode = 'none' | 'unused' | 'olderThan';
export type PruneBuildCacheMode = 'none' | 'unused' | 'all' | 'olderThan';

export interface PruneContainersOptions {
	mode: Exclude<PruneContainerMode, 'none'>;
	until?: string;
}

export interface PruneImagesOptions {
	mode: Exclude<PruneImageMode, 'none'>;
	until?: string;
}

export interface PruneVolumesOptions {
	mode: Exclude<PruneVolumeMode, 'none'>;
}

export interface PruneNetworksOptions {
	mode: Exclude<PruneNetworkMode, 'none'>;
	until?: string;
}

export interface PruneBuildCacheOptions {
	mode: Exclude<PruneBuildCacheMode, 'none'>;
	until?: string;
}

export interface SystemPruneRequest {
	containers?: PruneContainersOptions;
	images?: PruneImagesOptions;
	volumes?: PruneVolumesOptions;
	networks?: PruneNetworksOptions;
	buildCache?: PruneBuildCacheOptions;
}

export type PruneType = 'containers' | 'images' | 'networks' | 'volumes' | 'buildCache';

// --- GitOps: repositories, syncs, browsing ---

export interface GitRepositoryCreateDto {
	name: string;
	url: string;
	authType: string;
	username?: string;
	token?: string;
	sshKey?: string;
	sshHostKeyVerification?: string;
	commitAuthorName?: string;
	commitAuthorEmail?: string;
	signingKey?: string;
	signingKeyPassphrase?: string;
	description?: string;
	enabled?: boolean;
}

export interface GitRepositoryUpdateDto {
	name?: string;
	url?: string;
	authType?: string;
	username?: string;
	token?: string;
	sshKey?: string;
	sshHostKeyVerification?: string;
	commitAuthorName?: string;
	commitAuthorEmail?: string;
	signingKey?: string;
	signingKeyPassphrase?: string;
	description?: string;
	enabled?: boolean;
}

export interface GitRepository {
	id: string;
	name: string;
	url: string;
	authType: string;
	hasToken: boolean;
	hasSshKey: boolean;
	hasSigningKey: boolean;
	username?: string;
	sshHostKeyVerification?: string;
	commitAuthorName?: string;
	commitAuthorEmail?: string;
	description?: string;
	enabled: boolean;
	createdAt: string;
	updatedAt: string;
}

export type GitOpsSyncMode = 'deploy' | 'backup';

export type GitOpsBackupState = 'never' | 'pending' | 'backing_up' | 'backed_up' | 'paused' | 'failed' | 'needs_attention';

export type GitOpsBackupFailureReason =
	| 'repository'
	| 'auth'
	| 'project_missing'
	| 'snapshot'
	| 'unreadable_files'
	| 'limits'
	| 'conflict'
	| 'destination_occupied'
	| 'push_rejected';

export interface GitOpsSyncCreateDto {
	name: string;
	repositoryId: string;
	branch: string;
	composePath?: string;
	mode?: GitOpsSyncMode;
	projectId?: string;
	backupDirectory?: string;
	backupPaths?: string[];
	backupOnSave?: boolean;
	targetType?: string;
	projectName?: string;
	autoSync?: boolean;
	syncInterval?: number;
	syncDirectory?: boolean;
	pullImageAfterSync?: boolean;
	redeployAfterSync?: boolean;
	maxSyncFiles?: number;
	maxSyncTotalSize?: number;
	maxSyncBinarySize?: number;
	preDeployScriptPath?: string;
	preDeployRunnerImage?: string;
	preDeployEnv?: string;
	preDeployExtraMounts?: string;
	preDeployTimeoutSec?: number;
	preDeployNetworkMode?: string;
}

export interface GitOpsSyncUpdateDto {
	name?: string;
	repositoryId?: string;
	branch?: string;
	composePath?: string;
	targetType?: string;
	projectName?: string;
	backupPaths?: string[];
	backupOnSave?: boolean;
	autoSync?: boolean;
	syncInterval?: number;
	syncDirectory?: boolean;
	pullImageAfterSync?: boolean;
	redeployAfterSync?: boolean;
	maxSyncFiles?: number;
	maxSyncTotalSize?: number;
	maxSyncBinarySize?: number;
	preDeployScriptPath?: string;
	preDeployRunnerImage?: string;
	preDeployEnv?: string;
	preDeployExtraMounts?: string;
	preDeployTimeoutSec?: number;
	preDeployNetworkMode?: string;
}

export interface GitOpsSync {
	id: string;
	name: string;
	environmentId: string;
	repositoryId: string;
	repository?: GitRepository;
	branch: string;
	composePath: string;
	targetType?: string;
	mode: GitOpsSyncMode;
	backupDirectory?: string;
	backupPaths?: string[];
	backupOnSave: boolean;
	backupPending: boolean;
	backupState?: GitOpsBackupState;
	backupFailureReason?: GitOpsBackupFailureReason;
	lastBackupAt?: string;
	projectName: string;
	projectId?: string;
	autoSync: boolean;
	syncInterval: number;
	syncDirectory: boolean;
	pullImageAfterSync: boolean;
	redeployAfterSync: boolean;
	syncedFiles?: string;
	maxSyncFiles: number;
	maxSyncTotalSize: number;
	maxSyncBinarySize: number;
	lastSyncAt?: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	lastSyncCommit?: string;
	preDeployScriptPath?: string;
	preDeployRunnerImage?: string;
	preDeployEnv?: string;
	preDeployExtraMounts?: string;
	preDeployTimeoutSec: number;
	preDeployNetworkMode: string;
	preDeployLastRunAt?: string;
	preDeployLastRunStatus?: string;
	preDeployLastRunOutput?: string;
	createdAt: string;
	updatedAt: string;
}

export interface GitOpsSyncCounts {
	totalSyncs: number;
	activeSyncs: number;
	successfulSyncs: number;
	deploySyncs: number;
	backupSyncs: number;
}

export interface SyncResult {
	success: boolean;
	message: string;
	error?: string;
	syncedAt: string;
}

export interface FileTreeNode {
	name: string;
	path: string;
	type: string;
	size?: number;
	children?: FileTreeNode[];
}

export interface BrowseResponse {
	path: string;
	files: FileTreeNode[];
}

export interface SyncStatus {
	id: string;
	autoSync: boolean;
	nextSyncAt?: string;
	lastSyncAt?: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	lastSyncCommit?: string;
	mode: GitOpsSyncMode;
	backupState?: GitOpsBackupState;
	backupPending: boolean;
	backupFailureReason?: GitOpsBackupFailureReason;
	lastBackupAt?: string;
}

export type GitOpsBackupPreviewState = 'clean' | 'changes' | 'conflict' | 'destination_occupied';

export interface GitOpsBackupFileChange {
	path: string;
	change: 'added' | 'modified' | 'removed';
}

export interface GitOpsBackupPreview {
	state: GitOpsBackupPreviewState;
	remoteCommit?: string;
	changes: GitOpsBackupFileChange[];
	conflicts: GitOpsBackupFileChange[];
	files: string[];
}

export interface GitOpsBackupHistoryEntry {
	commit: string;
	author: string;
	message: string;
	date: string;
	files: string[];
}

export interface GitOpsBackupHistoryResponse {
	entries: GitOpsBackupHistoryEntry[];
}

export interface GitOpsBackupFileDiff {
	path: string;
	patch: string;
}

export interface GitOpsBackupRevision {
	entry: GitOpsBackupHistoryEntry;
	diffs: GitOpsBackupFileDiff[];
}

export interface ResolveGitOpsBackupConflictRequest {
	strategy: 'use_arcane';
}

export interface GitRepositoryTestResponse {
	message: string;
}

export interface BranchInfo {
	name: string;
	isDefault: boolean;
}

export interface BranchesResponse {
	branches: BranchInfo[];
}

export interface ImportGitOpsSyncRequest {
	syncName: string;
	gitRepo: string;
	branch: string;
	dockerComposePath: string;
	autoSync: boolean;
	syncInterval: number;
}

export interface ImportGitOpsSyncResponse {
	successCount: number;
	failedCount: number;
	errors: string[];
}

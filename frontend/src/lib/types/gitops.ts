export interface GitRepository {
	id: string;
	name: string;
	url: string;
	provider?: string;
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
	token?: string;
	sshKey?: string;
	signingKey?: string;
	signingKeyPassphrase?: string;
}

export type GitOpsSyncMode = 'deploy' | 'backup' | 'bidirectional';

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

export interface GitOpsSync {
	id: string;
	name: string;
	environmentId: string;
	environmentName?: string;
	repositoryId: string;
	repository?: GitRepository;
	branch: string;
	composePath: string;
	composeOverridePath?: string;
	envContent?: string;
	targetType: string;
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
	bidirectionalSyncs: number;
}

export interface GitOpsSyncResult {
	success: boolean;
	message: string;
	error?: string;
	syncedAt: string;
}

export type FileTreeNodeType = 'file' | 'directory';

export interface FileTreeNode {
	name: string;
	path: string;
	type: FileTreeNodeType;
	size?: number;
	children?: FileTreeNode[];
}

export interface GitOpsBrowseResponse {
	path: string;
	files: FileTreeNode[];
}

export interface GitOpsSyncStatus {
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

export type GitOpsResolveBackupConflictStrategy = 'use_arcane' | 'use_repository';

export interface GitOpsResolveBackupConflictRequest {
	strategy: GitOpsResolveBackupConflictStrategy;
}

export interface GitRepositoryTestResponse {
	message: string;
}

export interface GitBranchInfo {
	name: string;
	isDefault: boolean;
}

export interface GitBranchesResponse {
	branches: GitBranchInfo[];
}

export interface ImportGitOpsSyncRequest {
	syncName: string;
	gitRepo: string;
	branch: string;
	dockerComposePath: string;
	autoSync: boolean;
	syncInterval: number;
	syncDirectory?: boolean;
	maxSyncFiles?: number;
	maxSyncTotalSize?: number;
	maxSyncBinarySize?: number;
	projectName?: string;
	pullImageAfterSync?: boolean;
	redeployAfterSync?: boolean;
	preDeployScriptPath?: string;
	preDeployRunnerImage?: string;
	preDeployEnv?: string;
	preDeployExtraMounts?: string;
	preDeployTimeoutSec?: number;
	preDeployNetworkMode?: string;
}

export interface ImportGitOpsSyncResponse {
	successCount: number;
	failedCount: number;
	errors: string[];
}

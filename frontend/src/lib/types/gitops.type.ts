export interface GitRepositoryCreateDto {
	name: string;
	url: string;
	authType: string;
	username?: string;
	token?: string;
	sshKey?: string;
	sshHostKeyVerification?: string;
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
	description?: string;
	enabled?: boolean;
}

export interface GitRepository {
	id: string;
	name: string;
	url: string;
	authType: string;
	username?: string;
	sshHostKeyVerification?: string;
	description?: string;
	enabled: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface GitOpsSyncCreateDto {
	name: string;
	repositoryId: string;
	branch: string;
	composePath: string;
	projectName?: string;
	autoSync?: boolean;
	syncInterval?: number;
	enabled?: boolean;
}

export interface GitOpsSyncUpdateDto {
	name?: string;
	repositoryId?: string;
	branch?: string;
	composePath?: string;
	projectName?: string;
	autoSync?: boolean;
	syncInterval?: number;
	enabled?: boolean;
}

export interface GitOpsSync {
	id: string;
	name: string;
	environmentId: string;
	repositoryId: string;
	repository?: GitRepository;
	branch: string;
	composePath: string;
	projectName: string;
	projectId?: string;
	autoSync: boolean;
	syncInterval: number;
	lastSyncAt?: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	lastSyncCommit?: string;
	enabled: boolean;
	createdAt: string;
	updatedAt: string;
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
	enabled: boolean;
	autoSync: boolean;
	nextSyncAt?: string;
	lastSyncAt?: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	lastSyncCommit?: string;
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
	enabled: boolean;
}

export interface ImportGitOpsSyncResponse {
	successCount: number;
	failedCount: number;
	errors: string[];
}

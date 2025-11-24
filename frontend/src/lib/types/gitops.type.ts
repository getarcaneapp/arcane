export interface GitOpsRepository {
	id: string;
	url: string;
	branch: string;
	username: string;
	composePath: string;
	projectName?: string;
	description?: string;
	autoSync: boolean;
	syncInterval: number;
	enabled: boolean;
	lastSyncedAt?: string;
	lastSyncStatus?: string;
	lastSyncError?: string;
	createdAt: string;
	updatedAt: string;
}

export interface GitOpsRepositoryCreateDto {
	url: string;
	branch?: string;
	username?: string;
	token?: string;
	composePath: string;
	projectName?: string;
	description?: string;
	autoSync?: boolean;
	syncInterval?: number;
	enabled?: boolean;
}

export interface GitOpsRepositoryUpdateDto {
	url?: string;
	branch?: string;
	username?: string;
	token?: string;
	composePath?: string;
	projectName?: string;
	description?: string;
	autoSync?: boolean;
	syncInterval?: number;
	enabled?: boolean;
}

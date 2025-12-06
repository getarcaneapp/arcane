export type EnvironmentStatus = 'online' | 'offline' | 'error';

export type Environment = {
	id: string;
	name: string;
	apiUrl: string;
	status: EnvironmentStatus;
	enabled: boolean;
	lastSeen?: string;
	tags?: string[];
};

export interface CreateEnvironmentDTO {
	apiUrl: string;
	name: string;
	bootstrapToken?: string;
	tags?: string[];
}

export interface UpdateEnvironmentDTO {
	apiUrl?: string;
	name?: string;
	enabled?: boolean;
	bootstrapToken?: string;
	tags?: string[];
}

export type TagMode = 'any' | 'all';
export type StatusFilter = 'all' | 'online' | 'offline';
export type GroupBy = 'none' | 'status' | 'tags';

export interface EnvironmentFilter {
	id: string;
	userId: string;
	name: string;
	isDefault: boolean;
	searchQuery: string;
	selectedTags: string[];
	excludedTags: string[];
	tagMode: TagMode;
	statusFilter: StatusFilter;
	groupBy: GroupBy;
	createdAt?: string;
	updatedAt?: string;
}

export interface CreateEnvironmentFilterDTO {
	name: string;
	isDefault?: boolean;
	searchQuery?: string;
	selectedTags?: string[];
	excludedTags?: string[];
	tagMode?: TagMode;
	statusFilter?: StatusFilter;
	groupBy?: GroupBy;
}

export interface UpdateEnvironmentFilterDTO {
	name?: string;
	isDefault?: boolean;
	searchQuery?: string;
	selectedTags?: string[];
	excludedTags?: string[];
	tagMode?: TagMode;
	statusFilter?: StatusFilter;
	groupBy?: GroupBy;
}

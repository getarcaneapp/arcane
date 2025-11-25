export interface NetworkSettings {
	Networks: Record<
		string,
		{
			IPAddress?: string;
			Driver?: string;
			[key: string]: any;
		}
	>;
}

export interface ProjectService {
	container_id: string;
	name: string;
	status: string;
	image?: string;
	ports?: string[];
	networks?: string[];
	volumes?: string[];
	environment?: Record<string, string>;
	restart_count?: number;
	health?: string;
	networkSettings?: NetworkSettings;
}

export interface IncludeFile {
	path: string;
	relativePath: string;
	content: string;
}

export interface GitOpsInfo {
	repositoryId: string;
	url: string;
	branch: string;
	autoSync: boolean;
}

export interface Project {
	id: string;
	name: string;
	path: string;
	runningCount: string;
	serviceCount: string;
	status: string;
	statusReason?: string;
	updatedAt: string;
	createdAt: string;
	services?: ProjectService[];
	composeContent?: string;
	envContent?: string;
	includeFiles?: IncludeFile[];
	gitOps?: GitOpsInfo;
}

export interface ProjectStatusCounts {
	runningProjects: number;
	stoppedProjects: number;
	totalProjects: number;
}

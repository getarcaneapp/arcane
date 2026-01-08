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

export interface ServicePort {
	mode?: string;
	target: number;
	published?: string;
	protocol?: string;
}

export interface ServiceVolume {
	type: string;
	source: string;
	target: string;
	read_only?: boolean;
	volume?: Record<string, any>;
	bind?: Record<string, any>;
}

export interface ProjectService {
	name?: string;
	image?: string;
	container_name?: string;
	command?: string[] | string | null;
	entrypoint?: string[] | string | null;
	environment?: Record<string, string>;
	env_file?: string[];
	ports?: ServicePort[];
	volumes?: ServiceVolume[];
	networks?: Record<string, any>;
	restart?: string;
	depends_on?: Record<string, any>;
	labels?: Record<string, string>;
	healthcheck?: Record<string, any>;
	deploy?: Record<string, any>;
	[key: string]: any;
}

export interface IncludeFile {
	path: string;
	relativePath: string;
	content: string;
}

// RuntimeService contains live container status information
export interface RuntimeService {
	name: string;
	image: string;
	status: string;
	containerId?: string;
	containerName?: string;
	ports?: string[];
	health?: string;
	serviceConfig?: ProjectService;
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
	gitOpsManagedBy?: string;
	lastSyncCommit?: string;
	gitRepositoryURL?: string;
	services?: ProjectService[];
	runtimeServices?: RuntimeService[];
	composeContent?: string;
	envContent?: string;
	includeFiles?: IncludeFile[];
}

export interface ProjectStatusCounts {
	runningProjects: number;
	stoppedProjects: number;
	totalProjects: number;
}

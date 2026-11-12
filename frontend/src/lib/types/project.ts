import type { ImageUpdateInfo } from './image';
import type { WorkspaceFileChange } from './workspace';

export interface ProjectIncludeFile {
	path: string;
	relativePath: string;
	content?: string;
}

export interface ServicePort {
	mode?: string;
	host_ip?: string;
	target: number;
	published?: string;
	protocol?: string;
}

export interface ServiceVolume {
	type: string;
	source: string;
	target: string;
	read_only?: boolean;
	volume?: Record<string, unknown>;
	bind?: Record<string, unknown>;
}

export interface ProjectService {
	name?: string;
	image?: string;
	build?: string | Record<string, unknown>;
	container_name?: string;
	command?: string[] | string | null;
	entrypoint?: string[] | string | null;
	environment?: Record<string, string>;
	env_file?: string[];
	ports?: ServicePort[];
	volumes?: ServiceVolume[];
	networks?: Record<string, unknown>;
	restart?: string;
	depends_on?: Record<string, unknown>;
	labels?: Record<string, string>;
	healthcheck?: Record<string, unknown>;
	deploy?: Record<string, unknown>;
	[key: string]: unknown;
}

export type ProjectTagSource = 'ui' | 'compose';

export type ProjectTagColor = 'gray' | 'purple' | 'blue' | 'green' | 'yellow' | 'orange' | 'red' | 'pink';

export interface ProjectTag {
	name: string;
	color: ProjectTagColor;
	sources: ProjectTagSource[];
}

export interface ProjectTagOption {
	name: string;
	color: ProjectTagColor;
}

export interface ProjectRuntimeService {
	name: string;
	image: string;
	status: string;
	containerId?: string;
	containerName?: string;
	ports?: string[];
	health?: string;
	iconLightUrl?: string;
	iconDarkUrl?: string;
	serviceConfig?: ProjectService;
	redeployDisabled?: boolean;
}

export interface ProjectServiceUpdateInfo {
	imageRef: string;
	updateInfo?: ImageUpdateInfo;
}

export interface ProjectUpdateInfo {
	status: 'has_update' | 'up_to_date' | 'not_pulled' | 'unknown' | 'error';
	hasUpdate: boolean;
	imageCount: number;
	checkedImageCount: number;
	imagesWithUpdates: number;
	imagesNotPulled: number;
	errorCount: number;
	errorMessage?: string;
	imageRefs?: string[];
	updatedImageRefs?: string[];
	notPulledImageRefs?: string[];
	updateInfoByRef?: Record<string, ImageUpdateInfo>;
	serviceUpdates?: Record<string, ProjectServiceUpdateInfo>;
	lastCheckedAt?: string;
}

export type ProjectConfigurationError = {
	code: 'env_file_unreadable';
	path: string;
	uid: number;
	gid: number;
	blocksOperations: boolean;
};

export interface Project {
	id: string;
	name: string;
	dirName?: string;
	relativePath?: string;
	projectsRoot?: string;
	path: string;
	iconLightUrl?: string;
	iconDarkUrl?: string;
	urls?: string[];
	tags: ProjectTag[];
	runningCount: number;
	serviceCount: number;
	status: string;
	statusReason?: string;
	configurationError?: ProjectConfigurationError;
	updatedAt: string;
	createdAt: string;
	isArchived: boolean;
	isDiscovered?: boolean;
	archivedAt?: string;
	gitOpsManagedBy?: string;
	lastSyncCommit?: string;
	gitRepositoryURL?: string;
	gitRepositoryProvider?: string;
	hasBuildDirective?: boolean;
	redeployDisabled?: boolean;
	activityId?: string;
	updateInfo?: ProjectUpdateInfo;
	services?: ProjectService[];
	runtimeServices?: ProjectRuntimeService[];
	composeContent?: string;
	composeFileName?: string;
	composeFiles?: string[];
	overrideContent?: string;
	overrideFileName?: string;
	envFileExists?: boolean;
	envContent?: string;
	includeFiles?: ProjectIncludeFile[];
}

export interface ProjectStatusCounts {
	runningProjects: number;
	stoppedProjects: number;
	totalProjects: number;
	archivedProjects: number;
	updatesAvailable: number;
}

export type ProjectDeployOptions = {
	pullPolicy?: 'missing' | 'always' | 'never';
	forceRecreate?: boolean;
	removeOrphans?: boolean;
	recreateVolumes?: boolean;
};

export interface ProjectWorkspaceUpdateManifest {
	fileChanges: WorkspaceFileChange[];
}

export interface ProjectsDirectoryStatus {
	path: string;
	containerPath: string;
	primary: boolean;
	enabled: boolean;
	exists: boolean;
	error?: string;
}

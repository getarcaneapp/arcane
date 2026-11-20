import type { SystemBackupDestination } from './backup';
import type { ImageUpdateInfo } from './image';
import type { BackupRun } from './ui/backup';
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

export interface ProjectPreStartHook {
	image?: string;
	command?: string[] | string | null;
	per_replica?: boolean;
	[key: string]: unknown;
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
	pre_start?: ProjectPreStartHook[];
	[key: string]: unknown;
}

export interface ProjectJobSchedule {
	cron: string;
	timezone?: string;
}

export interface ProjectJob {
	name: string;
	image?: string;
	profiles?: string[];
	schedules: ProjectJobSchedule[];
	manualAllowed: boolean;
	disabled: boolean;
	hasBuild: boolean;
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

export interface ProjectUrl {
	url: string;
	label?: string;
}

export interface Project {
	id: string;
	name: string;
	dirName?: string;
	relativePath?: string;
	projectsRoot?: string;
	path: string;
	iconLightUrl?: string;
	iconDarkUrl?: string;
	urls?: ProjectUrl[];
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
	gitOpsSyncMode?: string;
	lastSyncCommit?: string;
	gitRepositoryURL?: string;
	gitRepositoryProvider?: string;
	hasBuildDirective?: boolean;
	redeployDisabled?: boolean;
	activityId?: string;
	updateInfo?: ProjectUpdateInfo;
	services?: ProjectService[];
	runtimeServices?: ProjectRuntimeService[];
	jobs?: ProjectJob[];
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

export type ProjectTransferMode = 'copy' | 'move';

export interface ProjectTransferRequest {
	targetEnvironmentId: string;
	mode: ProjectTransferMode;
	name: string;
	includeVolumes: boolean;
	start: boolean;
}

export interface ProjectTransferVolume {
	key: string;
	sourceName: string;
	targetName: string;
}

export interface ProjectTransferCheck {
	code: string;
	severity: 'error' | 'warning';
	message: string;
	resource?: string;
}

export interface ProjectTransferResource {
	targetEnvironmentId: string;
	kind: 'volume' | 'network';
	name: string;
}

export interface ProjectTransferPreflight {
	projectName: string;
	volumes: ProjectTransferVolume[];
	checks: ProjectTransferCheck[];
	sourceRunning: boolean;
	allowed: boolean;
}

export interface ProjectTransferStarted {
	activityId: string;
}

export type ProjectVolumeKind = 'named' | 'external' | 'driverOpts';

export interface ProjectVolume {
	key: string;
	name: string;
	kind: ProjectVolumeKind;
	driver?: string;
	labels?: Record<string, string>;
	driverOpts?: Record<string, string>;
	archive?: string;
	included: boolean;
	missing?: boolean;
	inUse?: boolean;
	backupStore?: boolean;
}

export interface ProjectExportOptions {
	volumes: ProjectVolume[];
	running: boolean;
}

export interface ExportProjectArchiveRequest {
	volumes?: string[];
	stopProject?: boolean;
}

export interface CreateProjectBackupRequest {
	volumes?: string[];
	stopContainers?: boolean;
	destination?: SystemBackupDestination;
	s3DestinationId?: string;
}

export interface ProjectBackupVolume {
	key: string;
	name: string;
	included: boolean;
}

export interface RestoreProjectBackupRequest {
	files?: boolean;
	volumes?: string[];
	redeploy?: boolean;
}

export interface RestoreProjectBackupAsNewRequest {
	name: string;
	files?: boolean;
	volumes?: string[];
	start?: boolean;
}

export type ProjectBackup = BackupRun & {
	projectId: string;
	projectName: string;
	composeName: string;
	completedAt?: string;
	volumes: ProjectBackupVolume[];
	stoppedContainers: boolean;
};

export interface ImportProjectArchiveRequest {
	name: string;
	start?: boolean;
}

import type { VulnerabilityScanSummary } from './vulnerability.type';

export interface ImageUpdateInfoDto {
	hasUpdate: boolean;
	updateType: string;
	currentVersion: string;
	latestVersion: string;
	currentDigest: string;
	latestDigest: string;
	checkTime: string;
	responseTimeMs: number;
	error: string;
	authMethod?: 'none' | 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	authRegistry?: string;
	usedCredential?: boolean;
}

export interface ImageUsageCounts {
	imagesInuse: number;
	imagesUnused: number;
	totalImages: number;
	totalImageSize: number;
}

export interface ImageUsedByDto {
	type: 'project' | 'container';
	name: string;
	id?: string;
}

export interface ImageSummaryDto {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	created: number;
	size: number;
	virtualSize: number;
	labels: Record<string, unknown> | null;
	inUse: boolean;
	usedBy?: ImageUsedByDto[];
	repo: string;
	tag: string;
	updateInfo?: ImageUpdateInfoDto;
	vulnerabilityScan?: VulnerabilityScanSummary;
}

export interface ImageDetailSummaryDto {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	parent: string;
	comment: string;
	created: string; // ISO string
	dockerVersion: string;
	author: string;
	config: {
		exposedPorts?: Record<string, unknown>;
		env?: string[];
		cmd?: string[];
		volumes?: Record<string, unknown>;
		workingDir?: string;
		argsEscaped?: boolean;
	};
	architecture: string;
	os: string;
	size: number;
	graphDriver: {
		data: unknown | null;
		name: string;
	};
	rootFs: {
		type: string;
		layers: string[];
	};
	metadata: {
		lastTagTime: string;
	};
	descriptor: {
		mediaType: string;
		digest: string;
		size: number;
	};
}

export type ImageUpdateData = ImageUpdateInfoDto;

export type ImageBuildStatus = 'running' | 'success' | 'failed';

export interface ImageBuildRecord {
	id: string;
	environmentId: string;
	userId?: string;
	username?: string;
	status: ImageBuildStatus;
	provider?: string;
	contextDir: string;
	dockerfile?: string;
	target?: string;
	tags?: string[];
	platforms?: string[];
	buildArgs?: Record<string, string>;
	labels?: Record<string, string>;
	cacheFrom?: string[];
	cacheTo?: string[];
	noCache: boolean;
	pull: boolean;
	network?: string;
	isolation?: string;
	shmSize?: number;
	ulimits?: Record<string, string>;
	entitlements?: string[];
	privileged: boolean;
	extraHosts?: string[];
	push: boolean;
	load: boolean;
	digest?: string;
	errorMessage?: string;
	output?: string;
	outputTruncated: boolean;
	completedAt?: string;
	durationMs?: number;
	createdAt: string;
}

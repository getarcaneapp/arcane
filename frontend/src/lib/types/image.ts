import type { Paginated } from './base';
import type { VulnerabilityScanSummary } from './vulnerability';

export interface ImageListResponse extends Paginated<ImageSummary> {
	maxImageUploadSize?: number;
}

export interface ImageUpdateInfo {
	checkTime: string;
	updateType: string;
	currentVersion: string;
	latestVersion: string;
	currentDigest: string;
	latestDigest: string;
	currentCreated?: string;
	latestCreated?: string;
	error: string;
	authMethod?: 'none' | 'anonymous' | 'credential' | 'unknown';
	authUsername?: string;
	authRegistry?: string;
	responseTimeMs: number;
	hasUpdate: boolean;
	blocked?: boolean;
	blockedReason?: string;
	usedCredential?: boolean;
}

export interface ImageUsedBy {
	type: 'project' | 'container';
	name: string;
	id?: string;
}

export interface ImageSummary {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	pinnedReferences?: string[];
	created: number;
	size: number;
	virtualSize: number;
	labels: Record<string, unknown>;
	inUse: boolean;
	usedBy?: ImageUsedBy[];
	repo: string;
	tag: string;
	updateInfo?: ImageUpdateInfo;
	vulnerabilityScan?: VulnerabilityScanSummary;
	hardened?: HardenedImageInfo;
}

export type HardenedImageSupportStatus = 'supported' | 'ending_soon' | 'end_of_life' | 'unknown';

export interface HardenedImageInfo {
	name: string;
	title?: string;
	version?: string;
	definition?: string;
	distro?: string;
	variant?: string;
	flavor?: string;
	entitlement?: string;
	url?: string;
	chainId?: string;
	created?: string;
	compliance?: string[];
	/** Empty when the image has no shell; absent when unknown. */
	shell?: string;
	/** Empty when the image has no package manager; absent when unknown. */
	packageManager?: string;
	releaseDate?: string;
	endOfLifeDate?: string;
	supportStatus: HardenedImageSupportStatus;
}

export interface ImageAttestationList {
	imageRef: string;
	subjectDigest: string;
	platform?: string;
	attestations: ImageAttestation[];
}

export interface ImageReleaseNotes {
	imageRef: string;
	version?: string;
	status: 'found' | 'not_found' | 'unavailable' | 'link' | 'no_source';
	sourceUrl?: string;
	releaseUrl?: string;
	releaseName?: string;
	notes?: string;
	publishedAt?: string;
}

export interface ImageAttestation {
	digest: string;
	mediaType: string;
	artifactType?: string;
	predicateType: string;
	statementType?: string;
	subject: ImageAttestationSubject[];
	platform?: string;
	size: number;
	statement?: unknown;
}

export interface ImageAttestationSubject {
	name: string;
	digest: Record<string, string>;
}

export interface ImagePruneReport {
	imagesDeleted: string[];
	spaceReclaimed: number;
}

export interface ImageTagRequest {
	repository: string;
	tag?: string;
}

export interface ImageHistoryItem {
	id: string;
	created: number;
	createdBy: string;
	tags: string[];
	size: number;
	comment: string;
}

export interface ImageSearchResult {
	name: string;
	description: string;
	starCount: number;
	official: boolean;
	automated: boolean;
}

export interface ImageUsageCounts {
	imagesInuse: number;
	imagesUnused: number;
	totalImages: number;
	totalImageSize: number;
	hardenedEndingSoon?: number;
	hardenedEndOfLife?: number;
}

export interface ImageLoadResult {
	stream: string;
}

export interface ImageDetailSummary {
	id: string;
	repoTags: string[];
	repoDigests: string[];
	pinnedReferences?: string[];
	hardened?: HardenedImageInfo;
	updateInfo?: ImageUpdateInfo;
	comment: string;
	created: string;
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
		data: unknown;
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

export type ImageBuildRequest = {
	provider: 'local' | 'depot';
} & Pick<
	ImageBuildRecord,
	| 'contextDir'
	| 'dockerfile'
	| 'tags'
	| 'target'
	| 'buildArgs'
	| 'labels'
	| 'cacheFrom'
	| 'cacheTo'
	| 'noCache'
	| 'pull'
	| 'network'
	| 'isolation'
	| 'shmSize'
	| 'ulimits'
	| 'entitlements'
	| 'privileged'
	| 'extraHosts'
	| 'platforms'
	| 'push'
	| 'load'
>;

// --- Environments & deployment snippets ---

export type EnvironmentStatus = 'online' | 'standby' | 'offline' | 'error' | 'pending';

export type EdgeMTLSCertificate = {
	commonName?: string;
	expiresAt?: string;
	daysRemaining?: number;
	expired: boolean;
	expiringSoon: boolean;
};

export type Environment = {
	id: string;
	name: string;
	apiUrl: string;
	status: EnvironmentStatus;
	enabled: boolean;
	isEdge: boolean;
	edgeTransport?: 'grpc' | 'websocket';
	edgeSecurityMode?: 'token' | 'mtls';
	connected?: boolean;
	connectedAt?: string;
	lastHeartbeat?: string;
	lastPollAt?: string;
	lastSeen?: string;
	edgeMTLSCertificate?: EdgeMTLSCertificate;
	apiKey?: string;
};

export interface CreateEnvironmentDTO {
	apiUrl: string;
	name: string;
	bootstrapToken?: string;
	useApiKey?: boolean;
	isEdge?: boolean;
}

export interface UpdateEnvironmentDTO {
	apiUrl?: string;
	name?: string;
	enabled?: boolean;
	isEdge?: boolean;
	bootstrapToken?: string;
	regenerateApiKey?: boolean;
}

export interface DeploymentSnippetFile {
	name: string;
	content?: string;
	downloadUrl?: string;
	sensitive?: boolean;
	containerPath: string;
	permissions: string;
}

export interface DeploymentSnippetMTLS {
	dockerRun: string;
	dockerCompose: string;
	files: DeploymentSnippetFile[];
	hostDirHint: string;
}

export interface DeploymentSnippets {
	dockerRun: string;
	dockerCompose: string;
	mtls?: DeploymentSnippetMTLS;
}

// --- Webhooks ---

export type WebhookTargetType = 'container' | 'project' | 'updater' | 'gitops';
export type WebhookActionType = 'update' | 'start' | 'stop' | 'restart' | 'redeploy' | 'up' | 'down' | 'run' | 'sync';

export type Webhook = {
	id: string;
	name: string;
	tokenPrefix: string;
	targetType: WebhookTargetType;
	actionType: WebhookActionType;
	targetId: string;
	targetName?: string;
	environmentId: string;
	enabled: boolean;
	lastTriggeredAt?: string;
	createdAt: string;
};

export type WebhookCreated = {
	id: string;
	name: string;
	token: string;
	targetType: WebhookTargetType;
	actionType: WebhookActionType;
	targetId: string;
	createdAt: string;
};

export type CreateWebhook = {
	name: string;
	targetType: WebhookTargetType;
	actionType: WebhookActionType;
	targetId: string;
};

export type UpdateWebhook = {
	enabled: boolean;
};

// --- Vulnerability scanning ---

export type VulnerabilitySeverity = 'UNKNOWN' | 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
export type VulnerabilityScanStatus = 'pending' | 'scanning' | 'completed' | 'failed';

export interface CVSSInfo {
	v2Score?: number;
	v3Score?: number;
	v2Vector?: string;
	v3Vector?: string;
}

export interface Vulnerability {
	vulnerabilityId: string;
	pkgName: string;
	installedVersion: string;
	fixedVersion?: string;
	severity: VulnerabilitySeverity;
	title?: string;
	description?: string;
	references?: string[];
	cvss?: CVSSInfo;
	publishedDate?: string;
	lastModifiedDate?: string;
}

export interface VulnerabilityWithImage extends Vulnerability {
	imageId: string;
	imageName: string;
}

export interface SeveritySummary {
	critical: number;
	high: number;
	medium: number;
	low: number;
	unknown: number;
	total: number;
}

export interface EnvironmentVulnerabilitySummary {
	totalImages: number;
	scannedImages: number;
	summary?: SeveritySummary;
}

export interface VulnerabilityScanResult {
	imageId: string;
	imageName: string;
	scanTime: string;
	status: VulnerabilityScanStatus;
	scanPhase?: 'creating_container' | 'scanning_image' | 'storing_results';
	summary?: SeveritySummary;
	vulnerabilities?: Vulnerability[];
	error?: string;
	duration?: number;
	scannerVersion?: string;
}

export interface VulnerabilityScanSummary {
	imageId: string;
	scanTime: string;
	status: VulnerabilityScanStatus;
	scanPhase?: 'creating_container' | 'scanning_image' | 'storing_results';
	summary?: SeveritySummary;
	error?: string;
}

export interface ScanSummariesRequest {
	imageIds: string[];
}

export interface ScanSummariesResponse {
	summaries: Record<string, VulnerabilityScanSummary | undefined>;
}

export interface ScannerStatus {
	available: boolean;
	version?: string;
}

export interface IgnoreVulnerabilityPayload {
	imageId: string;
	vulnerabilityId: string;
	pkgName: string;
	installedVersion: string;
	reason?: string;
}

export interface IgnoredVulnerability {
	id: string;
	environmentId: string;
	imageId: string;
	vulnerabilityId: string;
	pkgName: string;
	installedVersion: string;
	reason?: string;
	createdBy: string;
	createdAt: string;
}

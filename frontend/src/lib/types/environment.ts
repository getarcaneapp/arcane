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
	lastEdgeTransport?: 'grpc' | 'websocket';
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
	accessToken?: string;
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
	ignored?: boolean;
	ignoreId?: string;
}

export type VulnerabilityRiskBand = 'none' | 'low' | 'medium' | 'high' | 'critical';
export type VulnerabilityScoreStatus = 'complete' | 'provisional' | 'unavailable';

export interface VulnerabilityPackageInsight {
	pkgName: string;
	severity: VulnerabilitySeverity;
	count: number;
	installedVersion: string;
	fixedVersion?: string;
}

export interface VulnerabilityScanInsights {
	riskScore: number;
	riskBand: VulnerabilityRiskBand;
	scoreStatus: VulnerabilityScoreStatus;
	highestCvss: number;
	highestVulnerabilityId?: string;
	fixableCount: number;
	knownExploitedCount: number;
	exposure: VulnerabilityExposure;
	topPackages: VulnerabilityPackageInsight[];
}

export type VulnerabilityExposure = 'running' | 'stopped' | 'unused' | 'unknown';

export interface VulnerabilityRiskTrendPoint {
	date: string;
	riskScore: number;
}

export interface VulnerabilityRiskDrivers {
	knownExploited: number;
	overdueKnownExploited: number;
	highEpss: number;
	exposedCriticalHigh: number;
	fixable: number;
	findings: number;
	imagesScanned: number;
	imagesTotal: number;
	scoredImages: number;
}

export interface VulnerabilityRiskExposureBreakdown {
	running: number;
	stopped: number;
	unused: number;
	unknown: number;
}

export interface VulnerabilityRiskScoreDriver {
	vulnerabilityId: string;
	imageName: string;
	pkgName: string;
	fixedVersion: string;
	exposure: VulnerabilityExposure;
	knownExploited: boolean;
	risk: number;
}

export interface VulnerabilityRiskImage {
	imageId: string;
	imageName: string;
	riskScore: number;
	riskBand: VulnerabilityRiskBand;
	scoreStatus: VulnerabilityScoreStatus;
	exposure: VulnerabilityExposure;
	runningContainers: number;
	knownExploited: number;
	critical: number;
	high: number;
	fixable: number;
	findings: number;
}

export interface VulnerabilityRiskFinding {
	vulnerabilityId: string;
	pkgName: string;
	title?: string;
	severity: VulnerabilitySeverity;
	cvss: number;
	risk: number;
	epss?: number;
	knownExploited: boolean;
	ransomware: boolean;
	kevDueDate?: string;
	imagesAffected: number;
	runningImagesAffected: number;
	fixedVersion?: string;
}

export interface VulnerabilityThreatIntelStatus {
	enabled: boolean;
	lastSyncedAt?: string;
	stale: boolean;
}

export interface VulnerabilityRiskOverview {
	riskScore: number;
	riskBand: VulnerabilityRiskBand;
	scoreStatus: VulnerabilityScoreStatus;
	delta7d?: number;
	trend: VulnerabilityRiskTrendPoint[];
	drivers: VulnerabilityRiskDrivers;
	drivers7dAgo?: VulnerabilityRiskDrivers;
	summary: SeveritySummary;
	exposure: VulnerabilityRiskExposureBreakdown;
	scoreDriver?: VulnerabilityRiskScoreDriver;
	riskiestImages: VulnerabilityRiskImage[];
	riskiestFindings: VulnerabilityRiskFinding[];
	prevalentFindings: VulnerabilityRiskFinding[];
	threatIntel: VulnerabilityThreatIntelStatus;
	computedAt: string;
}

export interface SeveritySummary {
	critical: number;
	high: number;
	medium: number;
	low: number;
	unknown: number;
	total: number;
}

export interface VulnerabilityScanResult {
	imageId: string;
	imageName: string;
	scanTime: string;
	status: VulnerabilityScanStatus;
	activityId?: string;
	scanPhase?: 'creating_container' | 'scanning_image' | 'storing_results';
	summary?: SeveritySummary;
	vulnerabilities?: Vulnerability[];
	error?: string;
	duration?: number;
	scannerVersion?: string;
	hasReport?: boolean;
	insights?: VulnerabilityScanInsights;
}

export interface VulnerabilityScanSummary {
	imageId: string;
	scanTime: string;
	status: VulnerabilityScanStatus;
	activityId?: string;
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

export interface BulkVulnerabilityFilters {
	search?: string;
	severity?: string;
	imageName?: string;
	fixAvailable?: boolean;
}

export interface BulkVulnerabilityActionResponse {
	affectedCount: number;
}

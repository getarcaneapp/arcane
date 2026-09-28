// Mirrors types/transfer/transfer.go.

export type TransferKind = 'project' | 'volume';
export type TransferMode = 'copy' | 'move';
export type TransferStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'needs_attention' | 'rolled_back';
export type TransferPhase =
	| 'pending'
	| 'revalidate'
	| 'reserve'
	| 'prepare'
	| 'stop'
	| 'copy'
	| 'cutover'
	| 'recover'
	| 'finished';
export type TransferResourceKind = 'volume' | 'project_dir';
export type TransferResourceStatus = 'pending' | 'copying' | 'verified' | 'failed';

export interface TransferRequest {
	kind: TransferKind;
	mode: TransferMode;
	destinationEnvironmentId: string;
	projectId?: string;
	volumeName?: string;
	destinationName: string;
	volumeMappings?: Record<string, string>;
}

export interface TransferPlannedResource {
	key: string;
	kind: TransferResourceKind;
	sourceName: string;
	destinationName: string;
	estimatedBytes: number;
	estimatedFiles: number;
}

export interface TransferConsumer {
	containerId: string;
	name: string;
	running: boolean;
	restartPolicy?: string;
	composeProject?: string;
	composeService?: string;
	stopTimeout?: number;
}

export interface TransferBlocker {
	code: string;
	message: string;
	resource?: string;
}

export interface TransferReviewItem extends TransferBlocker {
	required: boolean;
}

export interface TransferPlan {
	request: TransferRequest;
	sourceEnvironmentId: string;
	sourceEnvironmentName: string;
	destinationEnvironmentName: string;
	resources: TransferPlannedResource[];
	consumers: TransferConsumer[];
	destinationServices?: string[];
	blockers: TransferBlocker[];
	reviews: TransferReviewItem[];
	requiredAcknowledgements: string[];
	estimatedBytes: number;
	estimatedFiles: number;
	requiresDowntime: boolean;
	planHash: string;
}

export interface TransferCreateRequest {
	idempotencyKey: string;
	planHash: string;
	request: TransferRequest;
	acknowledgements: string[];
}

export interface TransferResourceProgress {
	key: string;
	kind: TransferResourceKind;
	sourceName: string;
	destinationName: string;
	status: TransferResourceStatus;
	attempt: number;
	bytesTransferred: number;
	bytesTotal: number;
	error?: string;
}

export interface TransferRecoveryReport {
	sourceRestored: boolean;
	destinationRemoved: boolean;
	incomplete?: string[];
	destinationUnknown: boolean;
}

export interface Transfer {
	id: string;
	kind: TransferKind;
	mode: TransferMode;
	sourceEnvironmentId: string;
	destinationEnvironmentId: string;
	status: TransferStatus;
	phase: TransferPhase;
	attempt: number;
	plan: TransferPlan;
	resources: TransferResourceProgress[];
	recordedConsumers?: TransferConsumer[];
	destinationProjectId?: string;
	destinationStartupAttempted: boolean;
	sourceHeld: boolean;
	cancelRequested: boolean;
	recovery?: TransferRecoveryReport;
	activityId?: string;
	error?: string;
	requestedBy: string;
	createdAt: string;
	updatedAt?: string;
	finishedAt?: string;
}

export interface TransferCleanupRequest {
	removeSourceProject?: boolean;
	removeSourceFiles?: boolean;
	removeSourceVolumes?: string[];
}

export interface TransferCleanupSkip {
	resource: string;
	reason: string;
}

export interface TransferCleanupResponse {
	removed: string[];
	skipped: TransferCleanupSkip[];
}

const TERMINAL_STATUSES: ReadonlySet<TransferStatus> = new Set(['succeeded', 'failed', 'canceled', 'rolled_back']);

/** True once no further automatic work happens for the status. */
export function isTransferTerminal(status: TransferStatus): boolean {
	return TERMINAL_STATUSES.has(status);
}

/** True when the transfer stopped on its own and waits for the user. */
export function isTransferSettled(status: TransferStatus): boolean {
	return isTransferTerminal(status) || status === 'needs_attention';
}

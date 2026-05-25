import { PersistedState } from 'runed';
import { m } from '$lib/paraglide/messages';
import type { ContainerStats, ContainerSummaryDto } from '$lib/types/docker';
import type { Environment, EnvironmentStatus } from '$lib/types/environment';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import type { ProjectUpdateInfo } from '$lib/types/swarm';
import type { SwarmServiceModeName, SwarmServiceModeSpec } from '$lib/types/swarm';
import type { VulnerabilityScanSummary } from '$lib/types/environment';

// --- Container stats math ---

export function calculateCPUPercent(stats: ContainerStats | null): number {
	if (!stats?.cpu_stats || !stats?.precpu_stats) return 0;

	const cpuDelta = stats.cpu_stats.cpu_usage.total_usage - (stats.precpu_stats.cpu_usage?.total_usage || 0);
	const systemDelta = stats.cpu_stats.system_cpu_usage - (stats.precpu_stats.system_cpu_usage || 0);

	if (systemDelta > 0 && cpuDelta > 0) {
		const cpuPercent = (cpuDelta / systemDelta) * 100.0;
		return Math.min(Math.max(cpuPercent, 0), 100);
	}
	return 0;
}

export function calculateMemoryPercent(stats: ContainerStats | null): number {
	if (!stats?.memory_stats) return 0;

	const usage = calculateMemoryUsage(stats);
	const limit = stats.memory_stats.limit || 0;

	if (limit > 0) {
		const percent = (usage / limit) * 100;
		return Math.min(Math.max(percent, 0), 100);
	}
	return 0;
}

export function calculateMemoryUsage(stats: ContainerStats | null): number {
	if (!stats?.memory_stats) return 0;

	const usage = stats.memory_stats.usage || 0;
	const inactiveFile = stats.memory_stats.stats?.inactive_file || 0;
	return Math.max(usage - inactiveFile, 0);
}

export function getContainerIpAddresses(container: ContainerSummaryDto): string[] {
	const networks = container.networkSettings?.networks;
	if (!networks) return [];

	const seen = new Set<string>();
	const ipAddresses: string[] = [];
	for (const networkName of Object.keys(networks).sort((a, b) => a.localeCompare(b))) {
		const ipAddress = networks[networkName]?.ipAddress?.trim();
		if (!ipAddress || seen.has(ipAddress)) continue;

		seen.add(ipAddress);
		ipAddresses.push(ipAddress);
	}

	return ipAddresses;
}

// --- Swarm service mode helpers ---

export type SwarmServiceModeBadgeVariant = 'green' | 'blue' | 'amber' | 'purple' | 'gray';

export function getSwarmServiceModeFromSpec(mode: SwarmServiceModeSpec | undefined): SwarmServiceModeName {
	if (mode?.Replicated) return 'replicated';
	if (mode?.Global !== undefined) return 'global';
	if (mode?.ReplicatedJob) return 'replicated-job';
	if (mode?.GlobalJob !== undefined) return 'global-job';
	return 'unknown';
}

export function getSwarmServiceModeLabel(mode: string): string {
	switch (mode) {
		case 'replicated':
			return m.swarm_service_mode_replicated();
		case 'global':
			return m.swarm_service_mode_global();
		case 'replicated-job':
			return m.swarm_service_mode_replicated_job();
		case 'global-job':
			return m.swarm_service_mode_global_job();
		default:
			return m.common_unknown();
	}
}

export function getSwarmServiceModeVariant(mode: string): SwarmServiceModeBadgeVariant {
	switch (mode) {
		case 'replicated':
			return 'blue';
		case 'global':
			return 'green';
		case 'replicated-job':
			return 'amber';
		case 'global-job':
			return 'purple';
		default:
			return 'gray';
	}
}

export function isSwarmServiceModeScalable(mode: string): boolean {
	return mode === 'replicated' || mode === 'replicated-job';
}

// --- Project update status display ---

type ProjectUpdateStatus = ProjectUpdateInfo['status'];
export type ProjectUpdateBadgeVariant = 'blue' | 'green' | 'gray' | 'red';

export function getProjectUpdateStatus(updateInfo?: ProjectUpdateInfo): ProjectUpdateStatus {
	return updateInfo?.status ?? 'unknown';
}

export function getProjectUpdateText(updateInfo?: ProjectUpdateInfo): string {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'has_update':
			return m.images_has_updates();
		case 'up_to_date':
			return m.image_update_up_to_date_title();
		case 'error':
			return m.common_error();
		default:
			return m.image_update_status_unknown();
	}
}

export function getProjectUpdateVariant(updateInfo?: ProjectUpdateInfo): ProjectUpdateBadgeVariant {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'has_update':
			return 'blue';
		case 'up_to_date':
			return 'green';
		case 'error':
			return 'red';
		default:
			return 'gray';
	}
}

export function getProjectUpdateTooltip(updateInfo?: ProjectUpdateInfo): string | undefined {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'error':
			return m.image_update_check_failed_title();
		case 'unknown':
			return m.image_update_click_to_check();
		default:
			return undefined;
	}
}

// --- Updates filter transforms ---

export const updatesFilter = 'has_update';

export function ensureUpdatesFilter<T extends SearchPaginationSortRequest>(options: T): T {
	return {
		...options,
		filters: {
			...(options.filters ?? {}),
			updates: updatesFilter
		}
	};
}

export function ensureStandaloneContainerUpdatesFilter<T extends SearchPaginationSortRequest>(options: T): T {
	const next = ensureUpdatesFilter(options);
	return {
		...next,
		filters: {
			...(next.filters ?? {}),
			standalone: true
		}
	};
}

// --- Image pull progress tracking ---

export type PullPhase = 'preparing' | 'downloading' | 'extracting' | 'verifying' | 'waiting' | 'complete' | 'error';

export interface LayerProgress {
	current: number;
	total: number;
	status: string;
}

export interface PullProgressState {
	progress: number;
	statusText: string;
	error: string;
	layers: Record<string, LayerProgress>;
}

export const showImageLayersState = new PersistedState('arcane-show-image-layers', false);

export function createPullProgressState(): PullProgressState {
	return {
		progress: 0,
		statusText: '',
		error: '',
		layers: {}
	};
}

export function isLayerComplete(status: string): boolean {
	const s = status.toLowerCase();
	return (
		s.includes('pull complete') ||
		s.includes('already exists') ||
		s.includes('downloaded newer image') ||
		s.includes('image is up to date')
	);
}

export function getPullPhase(status: string, isComplete = false, hasError = false): PullPhase {
	if (hasError) return 'error';
	if (isComplete) return 'complete';

	const s = status.toLowerCase();
	if (isLayerComplete(s)) return 'complete';
	if (s.includes('downloading')) return 'downloading';
	if (s.includes('extracting')) return 'extracting';
	if (s.includes('verifying') || s.includes('digest')) return 'verifying';
	if (s.includes('waiting')) return 'waiting';
	if (s.includes('pulling') || s.includes('pull')) return 'downloading';
	return 'preparing';
}

export function isDownloadingLine(data: unknown): boolean {
	if (!data || typeof data !== 'object') return false;

	const obj = data as Record<string, unknown>;
	const status = String(obj['status'] ?? '').toLowerCase();
	const pd = obj['progressDetail'] as Record<string, unknown> | undefined;

	if (pd && (typeof pd['total'] === 'number' || typeof pd['current'] === 'number')) return true;

	return (
		status.includes('downloading') ||
		status.includes('extracting') ||
		status.includes('pulling fs layer') ||
		status.includes('download complete') ||
		status.includes('pull complete')
	);
}

export function calculateOverallProgress(layers: Record<string, LayerProgress>): number {
	const entries = Object.values(layers);
	if (entries.length === 0) return 0;

	const totalLayers = entries.length;
	let weightedSum = 0;

	for (const layer of entries) {
		const s = (layer.status || '').toLowerCase();
		if (isLayerComplete(s)) {
			weightedSum += 1.0;
		} else if (s.includes('extracting')) {
			weightedSum += 0.95;
		} else if (s.includes('verifying')) {
			weightedSum += 0.92;
		} else if (s.includes('download complete')) {
			weightedSum += 0.85;
		} else if (layer.total > 0) {
			const downloadProgress = (layer.current / layer.total) * 0.85;
			weightedSum += Math.min(downloadProgress, 0.85);
		} else if (s.includes('downloading') || s.includes('pulling')) {
			weightedSum += 0.05;
		}
	}

	const overallProgress = (weightedSum / totalLayers) * 100;
	return Math.min(overallProgress, 100);
}

export function areAllLayersComplete(layers: Record<string, LayerProgress>): boolean {
	const entries = Object.values(layers);
	if (entries.length === 0) return false;

	return entries.every((l) => l.status && isLayerComplete(l.status));
}

export function getLayerStats(layers: Record<string, LayerProgress>, forceComplete = false) {
	const entries = Object.entries(layers);
	const total = entries.length;
	const completed = entries.filter(([_, l]) => isLayerComplete(l.status || '')).length;
	const effectiveCompleted = forceComplete ? total : completed;

	const downloading = entries.filter(([_, l]) => {
		const s = (l.status || '').toLowerCase();
		return s.includes('downloading') || s.includes('pulling');
	}).length;

	const extracting = entries.filter(([_, l]) => l.status?.toLowerCase().includes('extracting')).length;

	return { total, completed: effectiveCompleted, downloading, extracting };
}

export function isIndeterminatePhase(layers: Record<string, LayerProgress>, currentProgress: number): boolean {
	const stats = getLayerStats(layers);
	if (stats.total === 0) return false;

	const downloadComplete = stats.downloading === 0;
	const hasExtractingLayers = stats.extracting > 0;
	const notAllComplete = stats.completed < stats.total;

	return downloadComplete && hasExtractingLayers && notAllComplete && currentProgress < 95;
}

export function extractErrorMessage(data: unknown, fallbackMessage: string): string {
	if (!data || typeof data !== 'object') return fallbackMessage;

	const obj = data as Record<string, unknown>;
	if (!obj['error']) return '';

	if (typeof obj['error'] === 'string') return obj['error'];
	if (typeof obj['error'] === 'object' && obj['error'] !== null) {
		const errObj = obj['error'] as Record<string, unknown>;
		if (typeof errObj['message'] === 'string') return errObj['message'];
	}

	return fallbackMessage;
}

export function updateLayerFromStreamData(layers: Record<string, LayerProgress>, data: unknown): Record<string, LayerProgress> {
	if (!data || typeof data !== 'object') return layers;

	const obj = data as Record<string, unknown>;
	const id = obj['id'] as string | undefined;
	if (!id) return layers;

	const currentLayer = layers[id] || { current: 0, total: 0, status: '' };
	const status = obj['status'] as string | undefined;

	if (status) {
		currentLayer.status = status;
	}

	const progressDetail = obj['progressDetail'] as Record<string, unknown> | undefined;
	if (progressDetail) {
		const current = progressDetail['current'] as number | undefined;
		const total = progressDetail['total'] as number | undefined;
		if (typeof current === 'number') currentLayer.current = current;
		if (typeof total === 'number') currentLayer.total = total;
	}

	return { ...layers, [id]: currentLayer };
}

export function createPullStreamHandler(callbacks: {
	onStatusChange: (status: string) => void;
	onProgressChange: (progress: number) => void;
	onLayersChange: (layers: Record<string, LayerProgress>) => void;
	onError: (error: string) => void;
	onFirstDownload?: () => void;
	errorMessage: string;
}) {
	let layers: Record<string, LayerProgress> = {};
	let hasOpenedPopover = false;

	return (data: unknown) => {
		if (!data) return;

		if (!hasOpenedPopover && isDownloadingLine(data)) {
			hasOpenedPopover = true;
			callbacks.onFirstDownload?.();
		}

		const errorMsg = extractErrorMessage(data, callbacks.errorMessage);
		if (errorMsg) {
			callbacks.onError(errorMsg);
			return;
		}

		const obj = data as Record<string, unknown>;
		if (obj['status'] && typeof obj['status'] === 'string') {
			callbacks.onStatusChange(obj['status']);
		}

		layers = updateLayerFromStreamData(layers, data);
		callbacks.onLayersChange(layers);

		const progress = calculateOverallProgress(layers);
		callbacks.onProgressChange(progress);
	};
}

export function getAggregateStatus(layers: Record<string, LayerProgress>, fallbackStatus = '', isComplete = false): string {
	if (isComplete) return 'Pull complete';

	const entries = Object.values(layers);
	if (entries.length === 0) return fallbackStatus;

	if (areAllLayersComplete(layers)) return 'Pull complete';

	const stats = getLayerStats(layers);

	if (stats.downloading > 0 || stats.extracting > 0) return 'Pulling';

	const hasVerifying = entries.some(
		(l) => l.status?.toLowerCase().includes('verifying') || l.status?.toLowerCase().includes('digest')
	);
	if (hasVerifying) return 'Verifying checksum';

	const hasWaiting = entries.some((l) => l.status?.toLowerCase().includes('waiting'));
	if (hasWaiting) return 'Waiting';

	return fallbackStatus || 'Preparing';
}

export function getAggregatePullPhase(layers: Record<string, LayerProgress>, isComplete = false, hasError = false): PullPhase {
	if (hasError) return 'error';
	if (isComplete) return 'complete';

	const entries = Object.values(layers);
	if (entries.length === 0) return 'preparing';

	if (areAllLayersComplete(layers)) return 'complete';

	const stats = getLayerStats(layers);

	if (stats.downloading > 0 || stats.extracting > 0) return 'downloading';

	const hasVerifying = entries.some(
		(l) => l.status?.toLowerCase().includes('verifying') || l.status?.toLowerCase().includes('digest')
	);
	if (hasVerifying) return 'verifying';

	const hasWaiting = entries.some((l) => l.status?.toLowerCase().includes('waiting'));
	if (hasWaiting) return 'waiting';

	return 'preparing';
}

// --- Vulnerability scan polling ---

const SCAN_IN_PROGRESS_STATUSES = new Set(['pending', 'scanning']);

export type VulnerabilityScanPollOptions = {
	intervalMs?: number;
	maxAttempts?: number;
	onUpdate?: (summary: VulnerabilityScanSummary) => void;
	onComplete?: (summary: VulnerabilityScanSummary) => void;
	onError?: (error: unknown) => void;
};

export type VulnerabilityScanTracker = {
	cancel: () => void;
	promise: Promise<VulnerabilityScanSummary>;
};

export type VulnerabilityScanStabilizeOptions = {
	scanRequestedAt?: string | number | Date | null;
	failedRecheckDelayMs?: number;
	maxFailedRechecks?: number;
	staleFailureGraceMs?: number;
};

function delay(ms: number): Promise<void> {
	return new Promise((resolve) => {
		setTimeout(resolve, ms);
	});
}

export function toTimestampMs(value?: string | number | Date | null): number {
	if (value == null) return 0;
	if (value instanceof Date) {
		const ts = value.getTime();
		return Number.isFinite(ts) ? ts : 0;
	}
	if (typeof value === 'number') {
		return Number.isFinite(value) ? value : 0;
	}
	const ts = Date.parse(value);
	return Number.isFinite(ts) ? ts : 0;
}

export function isVulnerabilityScanInProgress(status?: string | null): boolean {
	if (!status) return false;
	return SCAN_IN_PROGRESS_STATUSES.has(status);
}

export function isLikelyStaleFailedSummary(
	summary: VulnerabilityScanSummary,
	scanRequestedAt?: string | number | Date | null,
	staleFailureGraceMs = 1500
): boolean {
	if (summary.status !== 'failed') return false;

	const requestedAtMs = toTimestampMs(scanRequestedAt);
	if (requestedAtMs <= 0) return false;

	const summaryTimeMs = toTimestampMs(summary.scanTime);
	const scanTimeLooksOld = summaryTimeMs <= 0 || summaryTimeMs + staleFailureGraceMs < requestedAtMs;
	const missingErrorDetails = !summary.error || summary.error.trim() === '';

	return scanTimeLooksOld || missingErrorDetails;
}

export async function stabilizeFailedVulnerabilitySummary(
	imageId: string,
	initialSummary: VulnerabilityScanSummary,
	fetchSummary: (imageId: string) => Promise<VulnerabilityScanSummary>,
	options: VulnerabilityScanStabilizeOptions = {}
): Promise<VulnerabilityScanSummary> {
	const { scanRequestedAt, failedRecheckDelayMs = 1000, maxFailedRechecks = 2, staleFailureGraceMs = 1500 } = options;

	let summary = initialSummary;
	if (!isLikelyStaleFailedSummary(summary, scanRequestedAt, staleFailureGraceMs)) {
		return summary;
	}

	const retries = Math.max(0, maxFailedRechecks);
	for (let attempt = 0; attempt < retries; attempt++) {
		await delay(failedRecheckDelayMs);
		summary = await fetchSummary(imageId);
		if (summary.status !== 'failed') {
			return summary;
		}
	}

	return summary;
}

export function startVulnerabilityScanPolling(
	imageId: string,
	fetchSummary: (imageId: string) => Promise<VulnerabilityScanSummary>,
	options: VulnerabilityScanPollOptions = {}
): () => void {
	const { intervalMs = 2000, maxAttempts = 0, onUpdate, onComplete, onError } = options;
	let attempts = 0;
	let cancelled = false;
	let timeoutId: ReturnType<typeof setTimeout> | null = null;

	const schedule = () => {
		if (cancelled) return;
		timeoutId = setTimeout(run, intervalMs);
	};

	const run = async () => {
		if (cancelled) return;
		attempts += 1;
		try {
			const summary = await fetchSummary(imageId);
			onUpdate?.(summary);

			const isScanning = isVulnerabilityScanInProgress(summary?.status);
			if (!isScanning) {
				onComplete?.(summary);
				return;
			}
		} catch (error) {
			onError?.(error);
		}

		if (maxAttempts > 0 && attempts >= maxAttempts) {
			onError?.(new Error('Scan polling exceeded max attempts.'));
			return;
		}

		schedule();
	};

	void run();

	return () => {
		cancelled = true;
		if (timeoutId) {
			clearTimeout(timeoutId);
		}
	};
}

export function startVulnerabilityScanTracking(
	imageId: string,
	fetchSummary: (imageId: string) => Promise<VulnerabilityScanSummary>,
	options: VulnerabilityScanPollOptions = {}
): VulnerabilityScanTracker {
	let resolvePromise: (summary: VulnerabilityScanSummary) => void;
	let rejectPromise: (error: unknown) => void;

	const promise = new Promise<VulnerabilityScanSummary>((resolve, reject) => {
		resolvePromise = resolve;
		rejectPromise = reject;
	});

	const cancel = startVulnerabilityScanPolling(imageId, fetchSummary, {
		...options,
		onComplete: (summary) => {
			options.onComplete?.(summary);
			if (summary.status === 'completed') {
				resolvePromise(summary);
			} else {
				rejectPromise(summary);
			}
		},
		onError: (error) => {
			options.onError?.(error);
			rejectPromise(error);
		}
	});

	return { cancel, promise };
}

// --- Arcane icon label extraction ---

const ARCANE_ICON_LABELS = ['arcane.icon', 'com.getarcaneapp.arcane.icon'];

export function getArcaneIconUrlFromLabels(labels?: Record<string, string> | null): string | null {
	if (!labels) return null;

	for (const [key, value] of Object.entries(labels)) {
		const normalizedKey = key.trim().toLowerCase();
		if (ARCANE_ICON_LABELS.includes(normalizedKey)) {
			const trimmed = value?.trim();
			if (trimmed) return trimmed;
		}
	}

	return null;
}

// --- App image URLs ---

export function getApplicationLogo(full = false, colorOverride?: string, version?: string): string {
	const params = new URLSearchParams();

	if (full) {
		params.set('full', 'true');
	}

	if (colorOverride) {
		params.set('color', colorOverride);
	}

	if (version) {
		params.set('v', version);
	}

	const query = params.toString();
	return query ? `/api/app-images/logo?${query}` : '/api/app-images/logo';
}

export function getDefaultProfilePicture(): string {
	return '/api/app-images/profile';
}

// --- Status variants ---

export type StatusVariant = 'red' | 'purple' | 'green' | 'blue' | 'gray' | 'amber';

const STATUS_VARIANT_MAP: Record<string, StatusVariant> = {
	running: 'green',
	deployed: 'green',
	stopped: 'red',
	failed: 'red',
	pending: 'amber',
	creating: 'blue',
	updating: 'blue',
	deleting: 'purple',
	exited: 'red'
};

export function getStatusVariant(status?: string | null): StatusVariant {
	if (!status) return 'gray';
	return STATUS_VARIANT_MAP[String(status).toLowerCase()] ?? 'gray';
}

export { STATUS_VARIANT_MAP as statusVariantMap };

// --- Environment status resolution ---

type RuntimeEnvironmentState = Pick<Environment, 'isEdge' | 'connected' | 'status' | 'lastPollAt'>;

export function resolveEnvironmentStatus(
	environment: RuntimeEnvironmentState,
	overrideStatus?: EnvironmentStatus | null
): EnvironmentStatus {
	const status = overrideStatus ?? environment.status;

	if (!environment.isEdge) {
		return status;
	}

	if (environment.connected === true) {
		return 'online';
	}

	if (environment.lastPollAt) {
		return 'standby';
	}

	if (status === 'pending') {
		return 'pending';
	}

	if (status === 'standby') {
		return 'standby';
	}

	if (environment.connected === false) {
		return 'offline';
	}

	return status;
}

export function isEnvironmentOnline(environment: RuntimeEnvironmentState, overrideStatus?: EnvironmentStatus | null): boolean {
	const resolved = resolveEnvironmentStatus(environment, overrideStatus);
	return resolved === 'online' || resolved === 'standby';
}

export function getEnvironmentStatusVariant(status: EnvironmentStatus): 'green' | 'blue' | 'amber' | 'red' {
	switch (status) {
		case 'online':
			return 'green';
		case 'standby':
			return 'blue';
		case 'pending':
			return 'amber';
		default:
			return 'red';
	}
}

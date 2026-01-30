/**
 * Utility for tracking Docker image pull progress
 */

import { PersistedState } from 'runed';

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

/**
 * Persisted state for whether to show layer details in pull progress UI
 */
export const showImageLayersState = new PersistedState('arcane-show-image-layers', false);

/**
 * Creates an initial pull progress state
 */
export function createPullProgressState(): PullProgressState {
	return {
		progress: 0,
		statusText: '',
		error: '',
		layers: {}
	};
}

/**
 * Checks if the status indicates a layer is complete
 */
export function isLayerComplete(status: string): boolean {
	const s = status.toLowerCase();
	return (
		s.includes('pull complete') ||
		s.includes('already exists') ||
		s.includes('downloaded newer image') ||
		s.includes('image is up to date')
	);
}

/**
 * Determines the phase of a pull operation from status text
 */
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

/**
 * Checks if a stream line indicates downloading activity (should open popover)
 */
export function isDownloadingLine(data: unknown): boolean {
	if (!data || typeof data !== 'object') return false;

	const obj = data as Record<string, unknown>;
	const status = String(obj?.status ?? '').toLowerCase();
	const pd = obj?.progressDetail as Record<string, unknown> | undefined;

	// Open if we see byte progress or any of the common pull statuses
	if (pd && (typeof pd.total === 'number' || typeof pd.current === 'number')) return true;

	return (
		status.includes('downloading') ||
		status.includes('extracting') ||
		status.includes('pulling fs layer') ||
		status.includes('download complete') ||
		status.includes('pull complete')
	);
}

/**
 * Calculates overall progress from layer progress data
 */
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
			// Extraction is the last step before completion
			weightedSum += 0.95;
		} else if (s.includes('verifying')) {
			weightedSum += 0.92;
		} else if (s.includes('download complete')) {
			weightedSum += 0.85;
		} else if (layer.total > 0) {
			// Download progress is weighted up to 85%
			const downloadProgress = (layer.current / layer.total) * 0.85;
			weightedSum += Math.min(downloadProgress, 0.85);
		} else if (s.includes('downloading') || s.includes('pulling')) {
			weightedSum += 0.05;
		}
	}

	const overallProgress = (weightedSum / totalLayers) * 100;
	return Math.min(overallProgress, 100);
}

/**
 * Checks if all layers are complete
 */
export function areAllLayersComplete(layers: Record<string, LayerProgress>): boolean {
	const entries = Object.values(layers);
	if (entries.length === 0) return false;

	return entries.every((l) => l.status && isLayerComplete(l.status));
}

/**
 * Calculates layer statistics
 */
export function getLayerStats(layers: Record<string, LayerProgress>, forceComplete = false) {
	const entries = Object.entries(layers);
	const total = entries.length;
	const completed = entries.filter(([_, l]) => isLayerComplete(l.status || '')).length;
	const effectiveCompleted = forceComplete ? total : completed;

	// Downloading: status contains 'downloading' or 'pulling' explicitly
	const downloading = entries.filter(([_, l]) => {
		const s = (l.status || '').toLowerCase();
		return s.includes('downloading') || s.includes('pulling');
	}).length;

	const extracting = entries.filter(([_, l]) => l.status?.toLowerCase().includes('extracting')).length;

	return { total, completed: effectiveCompleted, downloading, extracting };
}

/**
 * Checks if the pull is in an indeterminate phase (extracting/verifying with no measurable progress)
 * This is used to show a marquee-style progress bar
 */
export function isIndeterminatePhase(layers: Record<string, LayerProgress>, currentProgress: number): boolean {
	const stats = getLayerStats(layers);
	// If we have layers and most downloads are complete but extraction is happening
	// and progress is low (byte progress doesn't apply to extraction)
	if (stats.total === 0) return false;

	const downloadComplete = stats.downloading === 0;
	const hasExtractingLayers = stats.extracting > 0;
	const notAllComplete = stats.completed < stats.total;

	// We're in indeterminate phase if downloads are done, extraction is happening,
	// but not all layers are complete yet
	return downloadComplete && hasExtractingLayers && notAllComplete && currentProgress < 95;
}

/**
 * Extracts error message from stream data
 */
export function extractErrorMessage(data: unknown, fallbackMessage: string): string {
	if (!data || typeof data !== 'object') return fallbackMessage;

	const obj = data as Record<string, unknown>;
	if (!obj.error) return '';

	if (typeof obj.error === 'string') return obj.error;
	if (typeof obj.error === 'object' && obj.error !== null) {
		const errObj = obj.error as Record<string, unknown>;
		if (typeof errObj.message === 'string') return errObj.message;
	}

	return fallbackMessage;
}

/**
 * Updates layer progress from stream data
 */
export function updateLayerFromStreamData(layers: Record<string, LayerProgress>, data: unknown): Record<string, LayerProgress> {
	if (!data || typeof data !== 'object') return layers;

	const obj = data as Record<string, unknown>;
	const id = obj.id as string | undefined;
	if (!id) return layers;

	const currentLayer = layers[id] || { current: 0, total: 0, status: '' };
	const status = obj.status as string | undefined;

	if (status) {
		currentLayer.status = status;
	}

	const progressDetail = obj.progressDetail as Record<string, unknown> | undefined;
	if (progressDetail) {
		const current = progressDetail.current as number | undefined;
		const total = progressDetail.total as number | undefined;
		if (typeof current === 'number') currentLayer.current = current;
		if (typeof total === 'number') currentLayer.total = total;
	}

	return { ...layers, [id]: currentLayer };
}

/**
 * Creates a stream handler for pull progress
 */
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

		// Check for first download activity
		if (!hasOpenedPopover && isDownloadingLine(data)) {
			hasOpenedPopover = true;
			callbacks.onFirstDownload?.();
		}

		// Handle errors
		const errorMsg = extractErrorMessage(data, callbacks.errorMessage);
		if (errorMsg) {
			callbacks.onError(errorMsg);
			return;
		}

		// Update status text
		const obj = data as Record<string, unknown>;
		if (obj.status && typeof obj.status === 'string') {
			callbacks.onStatusChange(obj.status);
		}

		// Update layer progress
		layers = updateLayerFromStreamData(layers, data);
		callbacks.onLayersChange(layers);

		// Calculate and update overall progress
		const progress = calculateOverallProgress(layers);
		callbacks.onProgressChange(progress);
	};
}

/**
 * Returns a computed aggregate status string based on all layers
 */
export function getAggregateStatus(
	layers: Record<string, LayerProgress>,
	fallbackStatus = '',
	isComplete = false
): string {
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

/**
 * Returns an aggregate PullPhase for phase-based title system
 */
export function getAggregatePullPhase(
	layers: Record<string, LayerProgress>,
	isComplete = false,
	hasError = false
): PullPhase {
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

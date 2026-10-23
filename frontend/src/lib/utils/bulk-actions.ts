import { toast } from 'svelte-sonner';

import { markActivityToastShown } from '#lib/components/activity/activity-completion-toasts.js';
import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { runWithActivityBatchId } from '#lib/services/api-service.js';
import { activityStore } from '#lib/stores/activity.store.svelte.js';
import type { Activity } from '#lib/types/activity.type.js';
import type { Result } from '#lib/types/result.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { parseEnvironmentRowKey, parseRowKey } from '#lib/utils/environments.js';
import { streamCacheBuster } from '#lib/utils/streaming.js';
import { tryCatch } from '#lib/utils/try-catch.js';

/**
 * Shared helpers for table bulk operations (start/stop/remove/prune/…). These
 * consolidate the iterate-tally-toast-refresh-clear loop that was copy-pasted
 * across every `*-table.svelte`.
 */

export interface BulkOperationMessages {
	/** Toast shown when every item succeeded. Receives the number that succeeded. */
	success: (count: number) => string;
	/** Toast shown when some items succeeded and some failed. */
	partial: (success: number, total: number, failed: number) => string;
	/** Toast shown when every item failed. */
	failure: () => string;
}

export interface BulkOperationResult {
	total: number;
	success: number;
	failed: number;
}

interface RunBulkOperationOptions<T> {
	/** The ids or table row keys to operate on. A no-op when empty. */
	ids: string[];
	/** Runs the operation for a single id or row key. May throw — failures are tallied. */
	run: (key: string) => Promise<T>;
	messages: BulkOperationMessages;
	/** Toggle a loading flag around the whole run. */
	setLoading?: (loading: boolean) => void;
	/** Called once after the run (and toast), regardless of outcome — e.g. to refresh data. */
	onComplete?: (result: BulkOperationResult) => void | Promise<unknown>;
	/** Clears the table selection after completion. Always called when there were ids. */
	clearSelection?: () => void;
	/** Optional per-item failure side effect, such as an item-specific toast. */
	onItemFailure?: (id: string, error: Error) => void;
	/** Run operations one at a time instead of concurrently. Defaults to concurrent. */
	sequential?: boolean;
	/** The requests were accepted for background work; completion belongs to activities. */
	acceptanceOnly?: boolean;
	activityLink?: boolean;
	onItemSuccess?: (id: string, value: T) => void;
}

/**
 * Runs `run` for each id, tallies successes/failures, emits a single summary
 * toast (success / partial / failure), then refreshes and clears the selection.
 * The success toast links to the first resulting activity when one is present.
 */
async function runBulkOperation<T>({
	ids,
	run,
	messages,
	setLoading,
	onComplete,
	clearSelection,
	onItemFailure,
	sequential = false,
	acceptanceOnly = false,
	activityLink = true,
	onItemSuccess
}: RunBulkOperationOptions<T>): Promise<BulkOperationResult> {
	const total = ids?.length ?? 0;
	const result: BulkOperationResult = { total, success: 0, failed: 0 };
	if (total === 0) return result;

	let firstActivityId: string | undefined;
	const activityIds: string[] = [];
	const tally = (key: string, outcome: Result<T>) => {
		const { id } = parseRowKey(key);
		if (outcome.error) {
			result.failed += 1;
			onItemFailure?.(id, outcome.error);
		} else {
			result.success += 1;
			onItemSuccess?.(id, outcome.data as T);
			const activityId = acceptanceOnly ? (outcome.data as Activity).id : extractActivityId(outcome.data);
			if (activityId) {
				if (acceptanceOnly && activityLink) activityStore.acceptActivity(outcome.data as Activity);
				activityIds.push(activityId);
				firstActivityId ??= activityId;
			}
		}
	};

	setLoading?.(true);
	try {
		// Multi-item runs share a batch id per environment so the Activity Center renders one
		// batch row. Each request is STARTED inside the synchronous batch scope
		// (awaited outside it), so concurrent unrelated actions never inherit it.
		// streamCacheBuster instead of bare crypto.randomUUID: the latter is
		// secure-context-only (throws on plain-HTTP deployments), and the fallback
		// format matches the backend batch-id pattern ^[A-Za-z0-9_-]{1,64}$.
		const counts = new Map<string | undefined, number>();
		for (const key of ids) {
			const { environmentId } = parseRowKey(key);
			counts.set(environmentId, (counts.get(environmentId) ?? 0) + 1);
		}
		const batchIds = new Map(
			Array.from(counts, ([environmentId, count]) => [environmentId, count > 1 ? streamCacheBuster() : null])
		);
		const startRun = (key: string) => {
			const batchId = batchIds.get(parseRowKey(key).environmentId);
			return batchId ? runWithActivityBatchId(batchId, () => tryCatch(run(key))) : tryCatch(run(key));
		};
		if (sequential) {
			for (const id of ids) {
				tally(id, await startRun(id));
			}
		} else {
			const outcomes = await Promise.all(ids.map(async (id) => [id, await startRun(id)] as const));
			for (const [id, outcome] of outcomes) tally(id, outcome);
		}
	} finally {
		setLoading?.(false);
	}

	// The summary toast below covers every spawned activity; suppress the
	// per-activity completion toasts for all of them, not just the linked one.
	if (!acceptanceOnly) {
		for (const activityId of activityIds) markActivityToastShown(activityId);
	}

	if (result.failed === 0) {
		const options = activityLink ? activityToastOptions(firstActivityId, !acceptanceOnly) : undefined;
		if (acceptanceOnly) toast.info(messages.success(result.success), options);
		else toast.success(messages.success(result.success), options);
	} else if (result.success > 0) {
		const options = activityLink ? activityToastOptions(firstActivityId, !acceptanceOnly) : undefined;
		toast.warning(messages.partial(result.success, total, result.failed), options);
	} else {
		toast.error(messages.failure());
	}

	await onComplete?.(result);
	clearSelection?.();

	return result;
}

export interface BulkConfirmCheckbox {
	id: string;
	label: string;
	initialState?: boolean;
}

type BulkConfirmRun<T> =
	| {
			run: (id: string, checkboxStates: Record<string, boolean>) => Promise<T>;
	  }
	| {
			runInEnvironment: (id: string, environmentId: string, checkboxStates: Record<string, boolean>) => Promise<T>;
	  };

export type BulkConfirmAndRunOptions<T> = Omit<RunBulkOperationOptions<T>, 'run'> &
	BulkConfirmRun<T> & {
		title: string;
		message: string;
		confirmLabel: string;
		destructive?: boolean;
		/** Optional confirm-dialog checkboxes (e.g. force / remove volumes). */
		checkboxes?: BulkConfirmCheckbox[];
	};

function rowRunner<T>(options: BulkConfirmRun<T>, checkboxStates: Record<string, boolean>): (key: string) => Promise<T> {
	if ('runInEnvironment' in options) {
		return (key) => {
			const { id, environmentId } = parseEnvironmentRowKey(key);
			return options.runInEnvironment(id, environmentId, checkboxStates);
		};
	}
	return (key) => options.run(parseRowKey(key).id, checkboxStates);
}

/**
 * Opens a confirm dialog (with optional checkboxes) and, on confirm, runs the
 * bulk operation via {@link runBulkOperation}. A no-op when `ids` is empty.
 */
export function bulkConfirmAndRun<T>(options: BulkConfirmAndRunOptions<T>): void {
	const {
		ids,
		title,
		message,
		confirmLabel,
		destructive = false,
		checkboxes,
		messages,
		setLoading,
		onComplete,
		clearSelection,
		onItemFailure,
		sequential,
		acceptanceOnly,
		activityLink,
		onItemSuccess
	} = options;
	if (!ids || ids.length === 0) return;

	openConfirmDialog({
		title,
		message,
		checkboxes,
		confirm: {
			label: confirmLabel,
			destructive,
			action: async (checkboxStates) => {
				await runBulkOperation({
					ids,
					run: rowRunner(options, checkboxStates),
					messages,
					setLoading,
					onComplete,
					clearSelection,
					onItemFailure,
					sequential,
					acceptanceOnly,
					activityLink,
					onItemSuccess
				});
			}
		}
	});
}

export interface ConfirmAndRunOptions<T> {
	title: string;
	message: string;
	confirmLabel: string;
	destructive?: boolean;
	setLoading?: (loading: boolean) => void;
	run: () => Promise<T>;
	failureMessage: string;
	onSuccess?: (result: T) => void | Promise<void>;
}

export function confirmAndRun<T>({
	title,
	message,
	confirmLabel,
	destructive = false,
	setLoading,
	run,
	failureMessage,
	onSuccess
}: ConfirmAndRunOptions<T>): void {
	openConfirmDialog({
		title,
		message,
		confirm: {
			label: confirmLabel,
			destructive,
			action: async () => {
				// Flip loading before awaiting `run`, not after: these back row
				// spinners, and the request is the part worth showing progress for.
				setLoading?.(true);
				await handleApiResultWithCallbacks({
					result: await tryCatch(run()),
					message: failureMessage,
					setLoadingState: setLoading ?? (() => {}),
					onSuccess
				});
			}
		}
	});
}

export function hasAnyLoadingState<TStatus extends string>(
	actionStatus: Record<string, TStatus>,
	loadingState: Record<string, boolean>
): boolean {
	return Object.values(actionStatus).some((status) => status !== '') || Object.values(loadingState).some(Boolean);
}

import { toast } from 'svelte-sonner';
import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { handleApiResultWithCallbacks, tryCatch, type Result } from '$lib/utils/api';
import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

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

export interface RunBulkOperationOptions<T> {
	/** The ids to operate on. A no-op when empty. */
	ids: string[];
	/** Runs the operation for a single id. May throw — failures are tallied. */
	run: (id: string) => Promise<T>;
	messages: BulkOperationMessages;
	/** Toggle a loading flag around the whole run. */
	setLoading?: (loading: boolean) => void;
	/** Called once after the run (and toast), regardless of outcome — e.g. to refresh data. */
	onComplete?: (result: BulkOperationResult) => void | Promise<unknown>;
	/** Clears the table selection after completion. Always called when there were ids. */
	clearSelection?: () => void;
	/** Optional per-item failure side effect, such as an item-specific toast. */
	onItemFailure?: (id: string) => void;
	/** Run operations one at a time instead of concurrently. Defaults to concurrent. */
	sequential?: boolean;
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
	sequential = false
}: RunBulkOperationOptions<T>): Promise<BulkOperationResult> {
	const total = ids?.length ?? 0;
	const result: BulkOperationResult = { total, success: 0, failed: 0 };
	if (total === 0) return result;

	let firstActivityId: string | undefined;
	const tally = (id: string, outcome: Result<T>) => {
		if (outcome.error) {
			result.failed += 1;
			onItemFailure?.(id);
		} else {
			result.success += 1;
			if (!firstActivityId) firstActivityId = extractActivityId(outcome.data);
		}
	};

	setLoading?.(true);
	try {
		if (sequential) {
			for (const id of ids) {
				tally(id, await tryCatch(run(id)));
			}
		} else {
			const outcomes = await Promise.all(ids.map(async (id) => [id, await tryCatch(run(id))] as const));
			for (const [id, outcome] of outcomes) tally(id, outcome);
		}
	} finally {
		setLoading?.(false);
	}

	if (result.failed === 0) {
		toast.success(messages.success(result.success), activityToastOptions(firstActivityId));
	} else if (result.success > 0) {
		toast.warning(messages.partial(result.success, total, result.failed));
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

export interface BulkConfirmAndRunOptions<T> extends Omit<RunBulkOperationOptions<T>, 'run'> {
	title: string;
	message: string;
	confirmLabel: string;
	destructive?: boolean;
	/** Optional confirm-dialog checkboxes (e.g. force / remove volumes). */
	checkboxes?: BulkConfirmCheckbox[];
	/** Runs the operation for a single id, given the resolved checkbox states. */
	run: (id: string, checkboxStates: Record<string, boolean>) => Promise<T>;
}

/**
 * Opens a confirm dialog (with optional checkboxes) and, on confirm, runs the
 * bulk operation via {@link runBulkOperation}. A no-op when `ids` is empty.
 */
export function bulkConfirmAndRun<T>({
	ids,
	title,
	message,
	confirmLabel,
	destructive = false,
	checkboxes,
	run,
	messages,
	setLoading,
	onComplete,
	clearSelection,
	onItemFailure,
	sequential
}: BulkConfirmAndRunOptions<T>): void {
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
					run: (id) => run(id, checkboxStates),
					messages,
					setLoading,
					onComplete,
					clearSelection,
					onItemFailure,
					sequential
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
				handleApiResultWithCallbacks({
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

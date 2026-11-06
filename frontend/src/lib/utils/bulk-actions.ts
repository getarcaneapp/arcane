import { toast } from 'svelte-sonner';

import { markActivityToastShown } from '#lib/components/activity/activity-completion-toasts.js';
import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { runWithActivityBatchId } from '#lib/services/api-service.js';
import { activityStore } from '#lib/stores/activity.store.svelte.js';
import type { Activity } from '#lib/types/activity.js';
import type { ConfirmDialogOptions } from '#lib/types/ui/confirmdialog.js';
import type { Result } from '#lib/types/ui/result.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { parseEnvironmentRowKey, parseRowKey } from '#lib/utils/environments.js';
import { streamCacheBuster } from '#lib/utils/streaming.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export interface BulkOperationMessages {
	success: (count: number) => string;
	partial: (success: number, total: number, failed: number) => string;
	failure: () => string;
}

export interface BulkOperationResult {
	total: number;
	success: number;
	failed: number;
}

interface RunBulkOperationOptions<T> {
	ids: string[];
	/** Runs the operation for a single id or row key. May throw — failures are tallied. */
	run: (key: string) => Promise<T>;
	messages: BulkOperationMessages;
	setLoading?: (loading: boolean) => void;
	/** Called once after the run (and toast), regardless of outcome — e.g. to refresh data. */
	onComplete?: (result: BulkOperationResult) => void | Promise<unknown>;
	clearSelection?: () => void;
	onItemFailure?: (id: string, error: Error) => void;
	/** Run operations one at a time instead of concurrently. Defaults to concurrent. */
	sequential?: boolean;
	/** The requests were accepted for background work; completion belongs to activities. */
	acceptanceOnly?: boolean;
	activityLink?: boolean;
	onItemSuccess?: (id: string, value: T) => void;
}

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
	run: (checkboxStates: Record<string, boolean>) => Promise<T>;
	failureMessage: string;
	onSuccess?: (result: T) => void | Promise<void>;
	checkboxes?: ConfirmDialogOptions['checkboxes'];
}

export function confirmAndRun<T>({
	title,
	message,
	confirmLabel,
	destructive = false,
	setLoading,
	run,
	failureMessage,
	onSuccess,
	checkboxes
}: ConfirmAndRunOptions<T>): void {
	openConfirmDialog({
		title,
		message,
		checkboxes,
		confirm: {
			label: confirmLabel,
			destructive,
			action: async (checkboxStates) => {
				// Flip loading before awaiting `run`, not after: these back row
				// spinners, and the request is the part worth showing progress for.
				setLoading?.(true);
				await handleApiResultWithCallbacks({
					result: await tryCatch(run(checkboxStates)),
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

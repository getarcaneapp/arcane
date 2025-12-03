import { toast } from 'svelte-sonner';
import { tryCatch } from '$lib/utils/try-catch';
import { handleApiResultWithCallbacks } from '$lib/utils/api.util';

export interface RefreshTask<T> {
	fetch: () => Promise<T>;
	onSuccess: (data: T) => void;
	errorMessage: string;
}

/**
 * Executes multiple refresh tasks in parallel with unified loading state management.
 * 
 * @param tasks - Array of refresh tasks to execute
 * @param setLoading - Callback to update loading state
 */
export async function parallelRefresh<T extends Record<string, RefreshTask<any>>>(
	tasks: T,
	setLoading: (loading: boolean) => void
): Promise<void> {
	setLoading(true);
	
	const taskKeys = Object.keys(tasks);
	const completionStatus = Object.fromEntries(taskKeys.map(k => [k, true]));
	
	const updateLoading = (key: string, value: boolean) => {
		completionStatus[key] = value;
		const stillLoading = Object.values(completionStatus).some(v => v);
		setLoading(stillLoading);
	};

	await Promise.all(
		taskKeys.map(async (key) => {
			const task = tasks[key];
			handleApiResultWithCallbacks({
				result: await tryCatch(task.fetch()),
				message: task.errorMessage,
				setLoadingState: (value) => updateLoading(key, value),
				onSuccess: task.onSuccess
			});
		})
	);
}

/**
 * Simple refresh helper for single data fetch with error handling
 */
export async function simpleRefresh<T>(
	fetch: () => Promise<T>,
	onSuccess: (data: T) => void,
	errorMessage: string,
	setLoading: (loading: boolean) => void
): Promise<void> {
	setLoading(true);
	try {
		const data = await fetch();
		onSuccess(data);
	} catch (error) {
		console.error('Refresh failed:', error);
		toast.error(errorMessage);
	} finally {
		setLoading(false);
	}
}

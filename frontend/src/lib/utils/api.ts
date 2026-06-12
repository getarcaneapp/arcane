import { error as kitError } from '@sveltejs/kit';
import { toast } from 'svelte-sonner';
import { APIError } from '$lib/services/api-service';

// --- Result wrapper ---

type Success<T> = {
	data: T;
	error: null;
};

type Failure<E> = {
	data: null;
	error: E;
};

export type Result<T, E = Error> = Success<T> | Failure<E>;

export async function tryCatch<T, E = Error>(promise: Promise<T>): Promise<Result<T, E>> {
	try {
		const data = await promise;
		return { data, error: null };
	} catch (error) {
		return { data: null, error: error as E };
	}
}

// --- API error extraction ---

function extractServerMessage(data: any): string | undefined {
	const inner = data && typeof data === 'object' ? ((data as any).data ?? data) : data;
	if (typeof inner === 'string') return inner;
	if (!inner || typeof inner !== 'object') return undefined;

	const msg = (inner as any).error || (inner as any).message || (inner as any).detail || (inner as any).error_description;
	if (typeof msg === 'string' && msg.trim()) return msg;

	if (Array.isArray((inner as any).errors) && (inner as any).errors.length) {
		const first = (inner as any).errors[0];
		if (typeof first === 'string' && first.trim()) return first;
		if (first && typeof first === 'object') {
			const em = (first as any).message || (first as any).error;
			if (typeof em === 'string' && em.trim()) return em;
		}
	}

	return undefined;
}

export function extractApiErrorMessage(error: any): string {
	if (!error) return 'Unknown error';

	const respData = error?.response?.data;
	const serverMsg = extractServerMessage(respData);
	if (serverMsg) return serverMsg;

	const bodyMsg = extractServerMessage(error?.body);
	if (bodyMsg) return bodyMsg;

	if (typeof error?.error === 'string' && error.error.trim()) return error.error;
	if (typeof error?.reason === 'string' && error.reason.trim()) return error.reason;
	if (typeof error?.stderr === 'string' && error.stderr.trim()) return error.stderr;
	if (typeof error?.data === 'string' && error.data.trim()) return error.data;
	if (typeof error?.message === 'string' && error.message.trim()) return error.message;

	try {
		return JSON.stringify(error);
	} catch {
		return 'Unknown error';
	}
}

export async function handleApiResultWithCallbacks<T>({
	result,
	message,
	setLoadingState = () => {},
	onSuccess = async () => {},
	onError = async () => {}
}: {
	result: Result<T, Error>;
	message: string;
	setLoadingState?: (value: boolean) => void;
	onSuccess?: (data: T) => void | Promise<void>;
	onError?: (error: Error) => void | Promise<void>;
}) {
	try {
		setLoadingState(true);

		if (result.error) {
			console.error(`API Error: ${message}:`, result.error);
			if (!(result.error instanceof APIError) || result.error.status !== 403) {
				toast.error(message, { description: extractApiErrorMessage(result.error) });
			}
			await Promise.resolve(onError(result.error));
		} else {
			await Promise.resolve(onSuccess(result.data as T));
		}
	} finally {
		try {
			setLoadingState(false);
		} catch (e) {
			console.warn('Failed to clear loading state', e);
		}
	}
}

// --- Page load error helpers ---

function extractApiErrorStatus(err: unknown, fallbackStatus = 500): number {
	if (err && typeof err === 'object') {
		const maybeResponse = (err as { response?: { status?: unknown } }).response;
		const responseStatus = maybeResponse?.status;
		if (typeof responseStatus === 'number' && Number.isFinite(responseStatus)) {
			return responseStatus;
		}
	}

	if (err && typeof err === 'object') {
		const maybeStatus = (err as { status?: unknown }).status;
		if (typeof maybeStatus === 'number' && Number.isFinite(maybeStatus)) {
			return maybeStatus;
		}
		if (typeof maybeStatus === 'string') {
			const parsed = Number.parseInt(maybeStatus, 10);
			if (!Number.isNaN(parsed)) {
				return parsed;
			}
		}
	}

	return fallbackStatus;
}

export function throwPageLoadError(err: unknown, fallbackMessage: string): never {
	const status = extractApiErrorStatus(err);
	const message = extractApiErrorMessage(err) || fallbackMessage;
	kitError(status, message);
}

// --- Refresh helpers ---

export interface RefreshTask<T> {
	fetch: () => Promise<T>;
	onSuccess: (data: T) => void;
	errorMessage: string;
}

export async function parallelRefresh<T extends Record<string, RefreshTask<any>>>(
	tasks: T,
	setLoading: (loading: boolean) => void
): Promise<void> {
	setLoading(true);

	const taskKeys = Object.keys(tasks);
	const completionStatus = Object.fromEntries(taskKeys.map((k) => [k, true]));

	const updateLoading = (key: string, value: boolean) => {
		completionStatus[key] = value;
		const stillLoading = Object.values(completionStatus).some((v) => v);
		setLoading(stillLoading);
	};

	await Promise.all(
		taskKeys.map(async (key) => {
			const task = tasks[key as keyof T];
			if (!task) return;

			handleApiResultWithCallbacks({
				result: await tryCatch(task.fetch()),
				message: task.errorMessage,
				setLoadingState: (value) => updateLoading(key, value),
				onSuccess: task.onSuccess
			});
		})
	);
}

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

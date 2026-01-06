import type { Result } from './try-catch';
import { toast } from 'svelte-sonner';

function extractServerMessage(data: any): string | undefined {
	const inner = data && typeof data === 'object' ? ((data as any).data ?? data) : data;
	if (typeof inner === 'string') return inner;
	if (!inner || typeof inner !== 'object') return undefined;

	// Support both Arcane-style (error/message) and Huma RFC 7807 (detail)
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

	// Some callers pass `{ body: ... }`
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

function extractDockerErrorMessage(error: any): string {
	return extractApiErrorMessage(error);
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
			const dockerMsg = extractDockerErrorMessage(result.error);
			console.error(`API Error: ${message}:`, result.error);
			toast.error(message, { description: dockerMsg });
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

import type { Result } from './try-catch';
import { toast } from 'svelte-sonner';

function extractDockerErrorMessage(error: any): string {
	if (!error) return 'Unknown error';

	if (error.response && error.response.data) {
		if (typeof error.response.data === 'string') return error.response.data;
		if (error.response.data.error) return error.response.data.error;
	}

	if (error.body && error.body.error) return error.body.error;
	if (error.error) return error.error;
	if (error.reason) return error.reason;
	if (error.stderr) return error.stderr;
	if (error.data && typeof error.data === 'string') return error.data;
	if (error.message) return error.message;
	return JSON.stringify(error);
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

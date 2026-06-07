import type { HandleClientError } from '@sveltejs/kit';
import { extractApiErrorMessage } from '$lib/utils/api.util';

export const handleError: HandleClientError = async ({ error, message, status }) => {
	if (error && typeof error === 'object' && 'response' in error) {
		const responseStatus = (error as { response?: { status?: number } }).response?.status;
		const apiErrorMessage = extractApiErrorMessage(error) || message;
		message = apiErrorMessage;
		status = responseStatus || status;
		const requestUrl =
			(error as { request?: { url?: string } }).request?.url || (error as { config?: { url?: string } }).config?.url || 'unknown';
		console.error(`API error: ${requestUrl} - ${apiErrorMessage}`);
	} else {
		console.error(error);
	}

	return {
		message,
		status
	};
};

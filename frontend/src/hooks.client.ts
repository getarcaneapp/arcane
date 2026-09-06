import type { HandleClientError } from '@sveltejs/kit/hooks';
import { extractApiErrorMessage } from '#lib/utils/api.js';

export const handleError: HandleClientError = async ({ kind, error }) => {
	if (kind !== 'unknown') {
		return;
	}

	if (error && typeof error === 'object' && 'response' in error) {
		const responseStatus = (error as { response?: { status?: number } }).response?.status;
		const apiErrorMessage = extractApiErrorMessage(error);
		const requestUrl =
			(error as { request?: { url?: string } }).request?.url || (error as { config?: { url?: string } }).config?.url || 'unknown';
		console.error(`API error: ${requestUrl} - ${apiErrorMessage}`);

		return {
			message: apiErrorMessage || 'Internal Error',
			status: responseStatus ?? 500
		};
	}

	console.error(error);
};

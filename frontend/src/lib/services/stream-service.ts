import { streamCacheBuster } from '#lib/utils/streaming.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import BaseAPIService, { handleUnauthorizedResponseInternal } from './api-service';

export const STREAM_CHANNEL_ENVIRONMENTS = 'environments';
export const STREAM_CHANNEL_DASHBOARD = 'dashboard';
export const STREAM_CHANNEL_ACTIVITIES = 'activities';
export const STREAM_CHANNEL_EVENTS = 'events';
export const STREAM_CHANNEL_VERSION = 'version';

class StreamService extends BaseAPIService {
	getClientStreamUrl(channels: string[], params: Record<string, string> = {}): string {
		const baseUrl = this.api.defaults.baseURL.replace(/\/+$/, '');
		const search = new URLSearchParams({ ...params, channels: channels.join(',') });
		search.set('_', streamCacheBuster());
		return `${baseUrl}/stream?${search.toString()}`;
	}

	async openClientStream(
		signal: AbortSignal,
		channels: string[],
		params: Record<string, string> = {},
		retry = false
	): Promise<Response> {
		const response = await fetch(this.getClientStreamUrl(channels, params), {
			credentials: 'include',
			headers: { Accept: 'text/event-stream' },
			signal
		});
		if (response.status === 401) {
			if (response.body) await tryCatch(response.body.cancel());
			const action = await handleUnauthorizedResponseInternal('/stream', retry);
			if (action === 'retry') {
				return this.openClientStream(signal, channels, params, true);
			}
			if (action === 'redirect' || action === 'reload') {
				return new Promise<Response>(() => {});
			}
		}
		if (!response.ok) {
			throw new Error(`Client stream failed with status ${response.status}`);
		}
		const contentType = response.headers.get('Content-Type')?.split(';')[0]?.trim().toLowerCase();
		if (contentType !== 'text/event-stream') {
			if (response.body) await tryCatch(response.body.cancel());
			throw new Error('Client stream returned an unexpected content type');
		}
		return response;
	}
}

export const streamService = new StreamService();

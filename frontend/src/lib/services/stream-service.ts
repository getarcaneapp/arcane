import BaseAPIService, { handleUnauthorizedResponseInternal } from './api-service';
import { streamCacheBuster } from '#lib/utils/streaming';

/** Channels the multiplexed client stream can carry. Must match types/stream. */
export const STREAM_CHANNEL_ENVIRONMENTS = 'environments';
export const STREAM_CHANNEL_DASHBOARD = 'dashboard';
export const STREAM_CHANNEL_ACTIVITIES = 'activities';

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
			headers: { Accept: 'application/x-json-stream' },
			signal
		});
		if (response.status === 401) {
			// Nothing reads this body, so release its connection before deciding
			// what to do next.
			await response.body?.cancel().catch(() => {});
			const action = await handleUnauthorizedResponseInternal('/stream', retry);
			if (action === 'retry') {
				return this.openClientStream(signal, channels, params, true);
			}
			// The document is being replaced (login redirect, or the reload that
			// follows a backend self-update). Never settling stops the caller
			// from opening a fresh stream against a page that is going away.
			if (action === 'redirect' || action === 'reload') {
				return new Promise<Response>(() => {});
			}
		}
		if (!response.ok) {
			throw new Error(`Client stream failed with status ${response.status}`);
		}
		return response;
	}
}

export const streamService = new StreamService();

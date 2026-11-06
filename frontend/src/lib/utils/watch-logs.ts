import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte.js';
import { ReconnectingWebSocket } from '#lib/utils/ws.js';

export function attachProjectLogsToWatch(projectId: string, sinceEpochSeconds: number, environmentId: string): () => void {
	let active = true;

	const client = new ReconnectingWebSocket<string>({
		buildUrl: async () => {
			const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
			return `${protocol}://${window.location.host}/api/environments/${environmentId}/ws/projects/${projectId}/logs?follow=true&tail=all&since=${sinceEpochSeconds}&timestamps=false&format=json&batched=true`;
		},
		parseMessage: (evt) => {
			if (typeof evt.data !== 'string') return null;
			try {
				return JSON.parse(evt.data);
			} catch {
				return null;
			}
		},
		onMessage: (payload) => {
			if (!active || !payload) return;
			const entries = Array.isArray(payload) ? payload : [payload];
			for (const entry of entries) {
				appendLogEntryInternal(entry);
			}
		},
		shouldReconnect: () => active,
		maxBackoff: 10000
	});

	void client.connect();

	return () => {
		active = false;
		client.close();
	};
}

function appendLogEntryInternal(entry: unknown) {
	if (!entry || typeof entry !== 'object') {
		return;
	}
	const { service, message } = entry as { service?: unknown; message?: unknown };
	if (typeof message !== 'string' || message === '') {
		return;
	}
	const prefix = typeof service === 'string' && service !== '' ? `${service}  | ` : '';
	operationWatchStore.append(`${prefix}${message}`);
}

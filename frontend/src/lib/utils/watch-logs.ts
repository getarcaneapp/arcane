import { environmentStore } from '#lib/stores/environment.store.svelte';
import { operationWatchStore } from '#lib/stores/operation-watch.store.svelte';
import { ReconnectingWebSocket } from '#lib/utils/ws';

/**
 * Attaches the operation watch dialog to a project's live log stream, mirroring
 * the log phase of a non-detached `docker compose up`: each container line is
 * appended as `service  | message`. sinceEpochSeconds scopes the backlog to the
 * operation, so the dialog shows every line the containers wrote since the
 * operation began (application startup included) without dredging up history
 * from previous runs. Returns a detach function that closes the stream.
 */
export function attachProjectLogsToWatch(projectId: string, sinceEpochSeconds: number): () => void {
	let active = true;

	const client = new ReconnectingWebSocket<string>({
		buildUrl: async () => {
			const envId = await environmentStore.getCurrentEnvironmentId();
			const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
			return `${protocol}://${window.location.host}/api/environments/${envId}/ws/projects/${projectId}/logs?follow=true&tail=all&since=${sinceEpochSeconds}&timestamps=false&format=json&batched=true`;
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

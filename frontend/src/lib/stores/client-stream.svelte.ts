import { invalidate } from '$app/navigation';
import type { QueryClient } from '@tanstack/svelte-query';

import { STREAM_CHANNEL_EVENTS, streamService } from '#lib/services/stream-service.js';
import { createSSEStream } from '#lib/stores/sse-stream.svelte.js';
import { PODMAN_STATUS_DEPENDENCY } from '#lib/types/podman.js';

type StreamEnvelope = {
	type?: string;
	channel?: string;
	activity?: unknown;
	environment?: unknown;
	eventLog?: unknown;
	version?: unknown;
	upgrade?: unknown;
};

type ChannelSubscriber = {
	channel: string;
	onEvent(event: unknown): void;
	onConnected?(): void;
};

type EnvironmentChanges = {
	environmentId: string;
	epoch: string;
	kinds: Record<string, number>;
};

const CONTAINER_DOMAINS = ['containers', 'container', 'ports', 'projects', 'project'];
const IMAGE_DOMAINS = ['images', 'image', 'image-update', 'vulnerabilities'];
const VOLUME_DOMAINS = ['volumes', 'volume', 'volume-sizes', 'volume-backup-names', 'volume-backups'];
const NETWORK_DOMAINS = ['networks', 'network'];
const MANAGER_GLOBAL_DOMAINS = new Set(['users', 'roles', 'federated-credentials', 'api-keys']);

export const KIND_QUERY_DOMAINS: Record<string, readonly string[]> = {
	container: [...CONTAINER_DOMAINS, 'images', 'image', 'volumes', 'volume', ...NETWORK_DOMAINS],
	image: IMAGE_DOMAINS,
	project: ['projects', 'project', 'containers', 'container', 'gitops-syncs'],
	volume: VOLUME_DOMAINS,
	network: [...NETWORK_DOMAINS, 'containers', 'container'],
	system: ['system', ...CONTAINER_DOMAINS, ...IMAGE_DOMAINS, ...VOLUME_DOMAINS, ...NETWORK_DOMAINS, 'build-workspace'],
	user: ['users', 'roles'],
	federated: ['federated-credentials'],
	environment: ['environments', 'environment', 'environment-settings', 'settings', 'environment-jobs', 'job-runs', 'job-run'],
	webhook: ['webhooks'],
	lifecycle: ['system', 'environments', 'environment'],
	podman: ['podman', 'podman-pods', 'podman-quadlets', 'podman-secrets', ...CONTAINER_DOMAINS],
	swarm: ['swarm'],
	gitops: ['gitops-syncs', 'projects', 'project'],
	event: [],
	activity: ['backup-activities', 'volume-backups', 'environment-jobs', 'job-runs', 'job-run'],
	build: ['images', 'image', 'build-workspace'],
	vulnerability: ['vulnerabilities', 'images', 'image'],
	image_update: ['containers', 'container', 'projects', 'project', 'images', 'image', 'image-update']
};

const ALL_DOMAINS = [...new Set([...Object.values(KIND_QUERY_DOMAINS).flat(), ...MANAGER_GLOBAL_DOMAINS])];
const EVENTS_DOMAIN = 'events';
const ALL_KIND = 'all';
const MIN_GAP_MS = 2_000;
const BATCH_MS = 250;
const KEY_SEPARATOR = '\u001f';

function kindDomains(kind: string): readonly string[] {
	return kind === ALL_KIND ? ALL_DOMAINS : (KIND_QUERY_DOMAINS[kind] ?? []);
}

function createChangeInvalidator() {
	let queryClient: QueryClient | undefined;
	const revisions = new Map<string, EnvironmentChanges>();
	let seeded = false;
	const pending = new Map<string, number>();
	const lastRun = new Map<string, number>();
	let timer: ReturnType<typeof setTimeout> | undefined;

	function schedule() {
		if (timer !== undefined || pending.size === 0) return;
		timer = setTimeout(flush, Math.max(0, Math.min(...pending.values()) - Date.now()));
	}

	function flush() {
		timer = undefined;
		const now = Date.now();
		const due = new Map<string, Set<string>>();
		const dueGlobal = new Set<string>();
		let rediscover = false;
		for (const [key, at] of pending) {
			if (at > now) continue;
			pending.delete(key);
			lastRun.set(key, now);
			const [environmentId = '', kind = ''] = key.split(KEY_SEPARATOR);
			rediscover ||= kind === ALL_KIND;
			const domains = kindDomains(kind);
			due.set(environmentId, new Set([...(due.get(environmentId) ?? []), ...domains]));
			for (const domain of domains) {
				if (MANAGER_GLOBAL_DOMAINS.has(domain)) dueGlobal.add(domain);
			}
		}
		if (queryClient && due.size > 0) {
			void queryClient.invalidateQueries({
				refetchType: 'active',
				predicate: ({ queryKey: [domain, scope] }) =>
					typeof domain === 'string' &&
					typeof scope === 'string' &&
					scope.split(',').some((environmentId) => due.get(environmentId)?.has(domain))
			});
			for (const domain of dueGlobal) {
				void queryClient.invalidateQueries({ queryKey: [domain], refetchType: 'active' });
			}
			void queryClient.invalidateQueries({ queryKey: [EVENTS_DOMAIN], refetchType: 'active' });
			if (rediscover) void invalidate(PODMAN_STATUS_DEPENDENCY);
		}
		schedule();
	}

	function queue(environmentId: string, kinds: string[]) {
		const now = Date.now();
		for (const kind of kinds) {
			const key = `${environmentId}${KEY_SEPARATOR}${kind}`;
			if (pending.has(key)) continue;
			pending.set(key, Math.max(now + BATCH_MS, (lastRun.get(key) ?? 0) + MIN_GAP_MS));
		}
		schedule();
	}

	function apply(changes: EnvironmentChanges[]) {
		for (const change of changes) {
			const known = revisions.get(change.environmentId);
			revisions.set(change.environmentId, change);
			if (!seeded) continue;
			if (known && known.epoch !== change.epoch) {
				queue(change.environmentId, [ALL_KIND]);
				continue;
			}
			const rose = Object.keys(change.kinds).filter((kind) => (change.kinds[kind] ?? 0) > (known?.kinds[kind] ?? 0));
			if (rose.length > 0) queue(change.environmentId, rose);
		}
		seeded = true;
	}

	function reset() {
		clearTimeout(timer);
		timer = undefined;
		revisions.clear();
		pending.clear();
		lastRun.clear();
		seeded = false;
	}

	return {
		connect(client: QueryClient) {
			queryClient = client;
		},
		apply,
		reset,
		invalidateEnvironment(environmentId: string) {
			queue(environmentId, [ALL_KIND]);
		}
	};
}

function createClientStreamInternal() {
	const subscribers = new Set<ChannelSubscriber>();
	let params: Record<string, string> = {};
	const changes = createChangeInvalidator();

	function activeChannels(): string[] {
		return [...new Set([...subscribers].map((subscriber) => subscriber.channel))].sort();
	}

	const transport = createSSEStream<StreamEnvelope>({
		label: 'Client',
		openStream: (signal) => streamService.openClientStream(signal, activeChannels(), params),
		onConnected: () => {
			for (const subscriber of subscribers) {
				subscriber.onConnected?.();
			}
		},
		onEvent: (envelope) => {
			const payload = envelope.activity ?? envelope.environment ?? envelope.eventLog ?? envelope.version ?? envelope.upgrade;
			if (!envelope.channel || payload === undefined) {
				return;
			}
			for (const subscriber of subscribers) {
				if (subscriber.channel === envelope.channel) {
					subscriber.onEvent(payload);
				}
			}
		}
	});

	let syncPending = false;

	function scheduleSync() {
		if (syncPending) {
			return;
		}
		syncPending = true;
		setTimeout(() => {
			syncPending = false;
			if (activeChannels().length === 0) {
				if (transport.isStarted) {
					transport.stop();
				}
				return;
			}
			if (!transport.isStarted) {
				transport.markStarted();
				transport.connect(transport.nextGeneration());
				return;
			}
			transport.restart();
		}, 0);
	}

	function subscribe(channel: string, handlers: { onEvent(event: unknown): void; onConnected?(): void }): () => void {
		const subscriber: ChannelSubscriber = { channel, ...handlers };
		const hadChannel = activeChannels().includes(channel);
		subscribers.add(subscriber);

		if (!transport.isStarted || !hadChannel) {
			scheduleSync();
		}

		return () => {
			subscribers.delete(subscriber);
			if (!activeChannels().includes(channel)) {
				scheduleSync();
			}
		};
	}

	return {
		get streamConnected(): boolean {
			return transport.streamConnected;
		},
		get streamFailed(): boolean {
			return transport.streamFailed;
		},
		get generation(): number {
			return transport.generation;
		},
		get hasActiveStream(): boolean {
			return transport.hasActiveStream;
		},
		isCurrentGeneration: transport.isCurrentGeneration,
		subscribe,
		connect: changes.connect,
		watchChanges(): () => void {
			const unsubscribe = subscribe(STREAM_CHANNEL_EVENTS, {
				onEvent: (payload) => {
					const event = payload as { changes?: EnvironmentChanges[] };
					if (Array.isArray(event.changes)) changes.apply(event.changes);
				}
			});
			return () => {
				unsubscribe();
				changes.reset();
			};
		},
		invalidateEnvironment: changes.invalidateEnvironment,
		setParams(next: Record<string, string>) {
			const changed = JSON.stringify(next) !== JSON.stringify(params);
			params = next;
			if (changed && transport.isStarted) {
				scheduleSync();
			}
		},
		retry() {
			transport.restart({ clearFailure: true });
		},
		restart() {
			transport.restart();
		}
	};
}

export const clientStream = createClientStreamInternal();

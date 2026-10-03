import { streamService } from '#lib/services/stream-service.js';
import { createSSEStream } from '#lib/stores/sse-stream.svelte.js';

type StreamEnvelope = {
	type?: string;
	channel?: string;
	dashboard?: unknown;
	activity?: unknown;
	environment?: unknown;
	eventLog?: unknown;
	version?: unknown;
};

type ChannelSubscriber = {
	channel: string;
	onEvent(event: unknown): void;
	onConnected?(): void;
};

function createClientStreamInternal() {
	const subscribers = new Set<ChannelSubscriber>();
	let params: Record<string, string> = {};

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
			const payload = envelope.dashboard ?? envelope.activity ?? envelope.environment ?? envelope.eventLog ?? envelope.version;
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
		subscribe(channel: string, handlers: { onEvent(event: unknown): void; onConnected?(): void }): () => void {
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
		},
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

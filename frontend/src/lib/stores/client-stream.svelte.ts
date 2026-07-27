import { streamService } from '#lib/services/stream-service';
import { createJSONLineStream } from '#lib/stores/json-line-stream.svelte';

type StreamEnvelope = {
	type: string;
	channel?: string;
	dashboard?: unknown;
	activity?: unknown;
	environment?: unknown;
};

type ChannelSubscriber = {
	channel: string;
	onEvent(event: unknown): void;
	onConnected?(): void;
};

/**
 * The app's single server stream. Every live feed — environments, dashboard,
 * activities — is multiplexed over one connection.
 *
 * Browsers allow only six concurrent HTTP/1.1 requests per origin, and each
 * long-lived stream holds one for the life of the page. One connection also
 * means one reconnect and one heartbeat to reason about instead of three that
 * can disagree about whether the server is reachable.
 *
 * Channels are opened on demand rather than always-on because their costs are
 * nothing alike: environments is a single local query, while dashboard proxies
 * a request to every remote agent on every tick. Subscribing or unsubscribing
 * changes the URL, so the connection is reopened — the same mechanism the
 * dashboard already used when its debug flag changed.
 */
function createClientStreamInternal() {
	const subscribers = new Set<ChannelSubscriber>();
	let params: Record<string, string> = {};

	function activeChannels(): string[] {
		return [...new Set([...subscribers].map((subscriber) => subscriber.channel))].sort();
	}

	const transport = createJSONLineStream<StreamEnvelope>({
		label: 'Client',
		openStream: (signal) => streamService.openClientStream(signal, activeChannels(), params),
		onConnected: () => {
			for (const subscriber of subscribers) {
				subscriber.onConnected?.();
			}
		},
		onEvent: (envelope) => {
			const payload = envelope.dashboard ?? envelope.activity ?? envelope.environment;
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

	function reopen() {
		if (!transport.isStarted) {
			return;
		}
		if (activeChannels().length === 0) {
			// Nothing left to carry; drop the connection rather than hold an
			// idle one open with no channels.
			transport.stop();
			return;
		}
		transport.restart();
	}

	function ensureStarted() {
		if (transport.isStarted || activeChannels().length === 0) {
			return;
		}
		transport.markStarted();
		transport.connect(transport.nextGeneration());
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
		/** Subscribes to a channel, (re)opening the connection to include it. */
		subscribe(channel: string, handlers: { onEvent(event: unknown): void; onConnected?(): void }): () => void {
			const subscriber: ChannelSubscriber = { channel, ...handlers };
			const hadChannel = activeChannels().includes(channel);
			subscribers.add(subscriber);

			if (!transport.isStarted) {
				ensureStarted();
			} else if (!hadChannel) {
				reopen();
			}

			return () => {
				subscribers.delete(subscriber);
				if (!activeChannels().includes(channel)) {
					reopen();
				}
			};
		},
		/** Sets query params shared by the stream URL (e.g. the dashboard debug flag). */
		setParams(next: Record<string, string>) {
			const changed = JSON.stringify(next) !== JSON.stringify(params);
			params = next;
			if (changed) {
				reopen();
			}
		},
		/** Reopens the connection, clearing a give-up state so it retries. */
		retry() {
			transport.restart({ clearFailure: true });
		},
		restart() {
			transport.restart();
		}
	};
}

export const clientStream = createClientStreamInternal();

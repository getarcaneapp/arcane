import { browser } from '$app/env';

const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

export type JSONLineEventBase = {
	type: string;
};

export interface JSONLineStreamConfig<TEvent extends JSONLineEventBase> {
	/** Used in console warnings, e.g. 'Dashboard' / 'Activity' / 'Environment'. */
	label: string;
	openStream(signal: AbortSignal): Promise<Response>;
	/** Receives every event except 'heartbeat', which is connection state the transport owns. */
	onEvent(event: TEvent): void;
	/** Runs on each successful (re)connect, before any event is delivered. */
	onConnected?(): void;
}

/**
 * The NDJSON transport shared by every aggregate stream: connect, read lines,
 * reconnect with backoff, and tear down explicitly on page hide.
 *
 * It is deliberately free of domain state so the environment, dashboard and
 * activity streams cannot drift apart in how they handle a dropped connection.
 */
export function createJSONLineStream<TEvent extends JSONLineEventBase>(config: JSONLineStreamConfig<TEvent>) {
	let started = false;
	// A single aggregated stream carries every environment's events; per-env
	// connections would multiply requests and exhaust the browser's
	// 6-per-origin HTTP/1.1 limit.
	let streamAbortController: AbortController | null = null;
	let removePageLifecycleListeners: (() => void) | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let streamGeneration = 0;
	let _streamConnected = $state(false);
	let _streamFailed = $state(false);

	function nextGeneration(): number {
		streamGeneration += 1;
		return streamGeneration;
	}

	function isCurrentGeneration(generation: number): boolean {
		return streamGeneration === generation;
	}

	function clearReconnectTimer() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
	}

	function abortStream() {
		clearReconnectTimer();
		streamAbortController?.abort();
		streamAbortController = null;
		_streamConnected = false;
	}

	// Closing a tab leaves teardown to whenever the browser gets around to
	// dropping the socket. Until it does, the server keeps writing heartbeats
	// into a connection nobody is reading — so tear down explicitly instead.
	function watchPageLifecycle() {
		if (!browser || removePageLifecycleListeners) {
			return;
		}

		const onPageHide = () => abortStream();
		// A bfcache restore comes back with the stream already torn down.
		const onPageShow = (event: PageTransitionEvent) => {
			if (event.persisted && started && !streamAbortController) {
				void connectStream(nextGeneration());
			}
		};

		window.addEventListener('pagehide', onPageHide);
		window.addEventListener('pageshow', onPageShow);
		removePageLifecycleListeners = () => {
			window.removeEventListener('pagehide', onPageHide);
			window.removeEventListener('pageshow', onPageShow);
		};
	}

	async function connectStream(generation: number) {
		if (!browser || !isCurrentGeneration(generation)) {
			return;
		}

		// Overlapping connects would otherwise strand the previous controller:
		// abortStream() only ever reaches the newest one, so the older request
		// would keep an open connection the server has no way to notice.
		streamAbortController?.abort();

		const controller = new AbortController();
		streamAbortController = controller;
		try {
			const response = await config.openStream(controller.signal);
			if (!isCurrentGeneration(generation) || !response.body) {
				// The response body is live even though nobody will read it;
				// dropping it on the floor leaves the server streaming into a
				// connection that stays open until its TCP timers expire.
				controller.abort();
				if (streamAbortController === controller) {
					streamAbortController = null;
				}
				return;
			}

			_streamConnected = true;
			_streamFailed = false;
			reconnectAttempt = 0;
			config.onConnected?.();
			await readJSONLines(response.body, generation);
		} catch (error) {
			if (!controller.signal.aborted && isCurrentGeneration(generation)) {
				console.warn(`${config.label} stream disconnected:`, error);
			}
		} finally {
			if (streamAbortController === controller) {
				streamAbortController = null;
			}
			if (isCurrentGeneration(generation)) {
				_streamConnected = false;
				if (!controller.signal.aborted) {
					scheduleReconnect(generation);
				}
			} else if (!controller.signal.aborted) {
				// A superseded generation has no owner left to abort it.
				controller.abort();
			}
		}
	}

	async function readJSONLines(stream: ReadableStream<Uint8Array>, generation: number) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let buffer = '';

		try {
			while (isCurrentGeneration(generation)) {
				const { done, value } = await reader.read();
				if (done) {
					break;
				}

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split('\n');
				buffer = lines.pop() ?? '';
				for (const line of lines) {
					handleStreamLine(line);
				}
			}

			buffer += decoder.decode();
			if (buffer.trim()) {
				handleStreamLine(buffer);
			}
		} finally {
			// The loop also exits when the generation advances, with the stream
			// still open. Cancelling is what actually closes the fetch and lets
			// the server see the disconnect; releaseLock alone does not.
			await reader.cancel().catch(() => {});
			reader.releaseLock();
		}
	}

	function handleStreamLine(line: string) {
		const trimmed = line.trim();
		if (!trimmed) {
			return;
		}

		try {
			const event = JSON.parse(trimmed) as TEvent;
			if (event.type === 'heartbeat') {
				_streamConnected = true;
				return;
			}
			config.onEvent(event);
		} catch (error) {
			console.warn(`Failed to parse ${config.label.toLowerCase()} stream line:`, error);
		}
	}

	function scheduleReconnect(generation: number) {
		if (!browser || !started || !isCurrentGeneration(generation)) {
			return;
		}

		if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
			_streamFailed = true;
			return;
		}

		clearReconnectTimer();
		const delay = Math.min(1000 * 2 ** reconnectAttempt, MAX_RECONNECT_DELAY);
		reconnectAttempt += 1;
		reconnectTimer = setTimeout(() => {
			void connectStream(generation);
		}, delay);
	}

	return {
		get streamConnected(): boolean {
			return _streamConnected;
		},
		set streamConnected(value: boolean) {
			_streamConnected = value;
		},
		get streamFailed(): boolean {
			return _streamFailed;
		},
		get generation(): number {
			return streamGeneration;
		},
		get hasActiveStream(): boolean {
			return streamAbortController !== null;
		},
		get isStarted(): boolean {
			return started;
		},
		isCurrentGeneration,
		nextGeneration,
		/** Opens the stream. Callers that need a generation for their own guards should call nextGeneration() first and pass it. */
		connect(generation: number) {
			void connectStream(generation);
		},
		markStarted() {
			started = true;
			watchPageLifecycle();
		},
		stop(options?: { resetStreamFailed?: boolean }) {
			const wasStarted = started;
			started = false;
			removePageLifecycleListeners?.();
			removePageLifecycleListeners = null;
			nextGeneration();
			abortStream();
			reconnectAttempt = 0;
			if (options?.resetStreamFailed) {
				_streamFailed = false;
			}
			return wasStarted;
		},
		/** Tears the connection down and opens a fresh one under a new generation. */
		restart({ clearFailure = false }: { clearFailure?: boolean } = {}) {
			if (clearFailure) {
				_streamFailed = false;
				reconnectAttempt = 0;
			}
			abortStream();
			void connectStream(nextGeneration());
		}
	};
}

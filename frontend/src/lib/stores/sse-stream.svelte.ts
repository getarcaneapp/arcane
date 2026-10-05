import { browser } from '$app/env';

import { currentSessionSignal } from '#lib/services/api-service.js';
import type { SSEEventBase, SSEStreamConfig } from '#lib/types/stream.js';
import { tryCatch } from '#lib/utils/try-catch.js';

const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

export function createSSEStream<TEvent extends SSEEventBase>(config: SSEStreamConfig<TEvent>) {
	let started = false;
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

	function watchPageLifecycle() {
		if (!browser || removePageLifecycleListeners) {
			return;
		}

		const onPageHide = () => abortStream();
		const onPageShow = (event: PageTransitionEvent) => {
			if (event.persisted && started && !streamAbortController) {
				void connectStream(nextGeneration());
			}
		};
		const onVisibilityChange = () => {
			if (document.hidden) {
				abortStream();
				return;
			}
			if (started && !streamAbortController && !_streamFailed) {
				void connectStream(nextGeneration());
			}
		};

		window.addEventListener('pagehide', onPageHide);
		window.addEventListener('pageshow', onPageShow);
		document.addEventListener('visibilitychange', onVisibilityChange);
		removePageLifecycleListeners = () => {
			window.removeEventListener('pagehide', onPageHide);
			window.removeEventListener('pageshow', onPageShow);
			document.removeEventListener('visibilitychange', onVisibilityChange);
		};
	}

	async function connectStream(generation: number) {
		if (!browser || !isCurrentGeneration(generation)) {
			return;
		}
		// Reconnects and restarts in a background tab wait for visibilitychange.
		if (document.hidden) {
			return;
		}

		streamAbortController?.abort();

		const controller = new AbortController();
		streamAbortController = controller;
		try {
			const operationResult = await tryCatch(
				(async () => {
					const response = await config.openStream(controller.signal);
					if (controller.signal.aborted || !isCurrentGeneration(generation) || !response.body) {
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
					await readSSEFrames(response.body, generation, controller.signal);
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				if (!controller.signal.aborted && !currentSessionSignal().aborted && isCurrentGeneration(generation)) {
					console.warn(`${config.label} stream disconnected:`, error);
				}
			} else {
				return operationResult.data;
			}
		} finally {
			if (streamAbortController === controller) {
				streamAbortController = null;
			}
			if (isCurrentGeneration(generation)) {
				_streamConnected = false;
				// After logout, the next sign-in restarts streams through their stores.
				if (!controller.signal.aborted && !currentSessionSignal().aborted) {
					scheduleReconnect(generation);
				}
			} else if (!controller.signal.aborted) {
				controller.abort();
			}
		}
	}

	async function readSSEFrames(stream: ReadableStream<Uint8Array>, generation: number, signal: AbortSignal) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let line = '';
		let data: string[] = [];
		let skipLF = false;

		function consumeLine() {
			if (line === '') {
				if (data.length > 0) {
					handleStreamData(data.join('\n'), generation, signal);
				}
				data = [];
			} else if (line === 'data') {
				data.push('');
			} else if (line.startsWith('data:')) {
				let value = line.slice(5);
				if (value.startsWith(' ')) value = value.slice(1);
				data.push(value);
			}
			line = '';
		}

		function consumeText(text: string) {
			for (const character of text) {
				if (signal.aborted || !isCurrentGeneration(generation)) return;
				if (skipLF && character === '\n') {
					skipLF = false;
					continue;
				}
				skipLF = character === '\r';
				if (character === '\r' || character === '\n') {
					consumeLine();
				} else {
					line += character;
				}
			}
		}

		try {
			while (!signal.aborted && isCurrentGeneration(generation)) {
				const { done, value } = await reader.read();
				if (signal.aborted || !isCurrentGeneration(generation)) return;
				if (done) break;
				consumeText(decoder.decode(value, { stream: true }));
			}
			if (!signal.aborted && isCurrentGeneration(generation)) {
				consumeText(decoder.decode());
			}
		} finally {
			await tryCatch(reader.cancel());
			reader.releaseLock();
		}
	}

	function handleStreamData(line: string, generation: number, signal: AbortSignal) {
		if (signal.aborted || !isCurrentGeneration(generation)) return;
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
			console.warn(`Failed to parse ${config.label.toLowerCase()} stream data:`, error);
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

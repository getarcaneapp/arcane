import { browser } from '$app/env';

import { currentSessionSignal } from '#lib/services/api-service.js';
import type { SSEEventBase, SSEStreamConfig } from '#lib/types/stream.js';
import { createAbortLifecycle } from '#lib/utils/abort-lifecycle.js';
import { tryCatch } from '#lib/utils/try-catch.js';

const MAX_RECONNECT_DELAY = 15_000;
const MAX_RECONNECT_ATTEMPTS = 20;

export function createSSEStream<TEvent extends SSEEventBase>(config: SSEStreamConfig<TEvent>) {
	const lifecycle = createAbortLifecycle();
	let streamAbortController: AbortController | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let _streamConnected = $state(false);
	let _streamFailed = $state(false);

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

	function runSession(sessionSignal: AbortSignal) {
		if (browser) {
			window.addEventListener('pagehide', () => abortStream(), { signal: sessionSignal });
			window.addEventListener(
				'pageshow',
				(event: PageTransitionEvent) => {
					if (event.persisted && !streamAbortController) {
						void connectStream(sessionSignal);
					}
				},
				{ signal: sessionSignal }
			);
			document.addEventListener(
				'visibilitychange',
				() => {
					if (document.hidden) {
						abortStream();
						return;
					}
					if (!streamAbortController && !_streamFailed) {
						void connectStream(sessionSignal);
					}
				},
				{ signal: sessionSignal }
			);
		}
		void connectStream(sessionSignal);
		return abortStream;
	}

	async function connectStream(sessionSignal: AbortSignal) {
		if (!browser || sessionSignal.aborted) {
			return;
		}
		if (document.hidden) {
			return;
		}

		streamAbortController?.abort();

		const controller = new AbortController();
		streamAbortController = controller;
		const signal = AbortSignal.any([sessionSignal, controller.signal]);
		try {
			const operationResult = await tryCatch(
				(async () => {
					const response = await config.openStream(signal);
					if (signal.aborted || !response.body) {
						controller.abort();
						return;
					}

					_streamConnected = true;
					_streamFailed = false;
					reconnectAttempt = 0;
					config.onConnected?.();
					await readSSEFrames(response.body, signal);
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				if (!signal.aborted && !currentSessionSignal().aborted) {
					console.warn(`${config.label} stream disconnected:`, error);
				}
			} else {
				return operationResult.data;
			}
		} finally {
			if (streamAbortController === controller) {
				streamAbortController = null;
			}
			if (!signal.aborted) {
				_streamConnected = false;
				if (!currentSessionSignal().aborted) {
					scheduleReconnect(sessionSignal);
				}
			}
		}
	}

	async function readSSEFrames(stream: ReadableStream<Uint8Array>, signal: AbortSignal) {
		const reader = stream.getReader();
		const decoder = new TextDecoder();
		let line = '';
		let data: string[] = [];
		let skipLF = false;

		function consumeLine() {
			if (line === '') {
				if (data.length > 0) {
					handleStreamData(data.join('\n'), signal);
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
				if (signal.aborted) return;
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
			while (!signal.aborted) {
				const { done, value } = await reader.read();
				if (signal.aborted) return;
				if (done) break;
				consumeText(decoder.decode(value, { stream: true }));
			}
			if (!signal.aborted) {
				consumeText(decoder.decode());
			}
		} finally {
			await tryCatch(reader.cancel());
			reader.releaseLock();
		}
	}

	function handleStreamData(line: string, signal: AbortSignal) {
		if (signal.aborted) return;
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

	function scheduleReconnect(sessionSignal: AbortSignal) {
		if (!browser || sessionSignal.aborted) {
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
			void connectStream(sessionSignal);
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
		get hasActiveStream(): boolean {
			return streamAbortController !== null;
		},
		get isStarted(): boolean {
			return lifecycle.active;
		},
		start() {
			lifecycle.start(runSession);
		},
		stop(options?: { resetStreamFailed?: boolean }) {
			const wasStarted = lifecycle.active;
			lifecycle.stop();
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
			if (lifecycle.active) {
				lifecycle.start(runSession);
			}
		}
	};
}

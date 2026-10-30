export type AbortLifecycle = {
	readonly active: boolean;
	start(run: (signal: AbortSignal) => void | (() => void)): void;
	stop(): void;
};

export function createAbortLifecycle(): AbortLifecycle {
	let controller: AbortController | null = null;

	return {
		get active(): boolean {
			return controller !== null && !controller.signal.aborted;
		},
		start(run) {
			controller?.abort();
			const current = new AbortController();
			controller = current;
			const cleanup = run(current.signal);
			if (!cleanup) {
				return;
			}
			if (current.signal.aborted) {
				cleanup();
				return;
			}
			current.signal.addEventListener('abort', cleanup, { once: true });
		},
		stop() {
			controller?.abort();
			controller = null;
		}
	};
}

type VisibilityPollerOptions = {
	intervalMs: number;
	poll: () => void | Promise<void>;
	immediate?: boolean;
};

export type VisibilityPoller = {
	start: () => void;
	stop: () => void;
};

export function createVisibilityPoller({ intervalMs, poll, immediate = false }: VisibilityPollerOptions): VisibilityPoller {
	let interval: ReturnType<typeof setInterval> | null = null;
	let started = false;

	function clearPollInterval() {
		if (!interval) return;
		clearInterval(interval);
		interval = null;
	}

	function runPoll() {
		void poll();
	}

	function startPollInterval() {
		if (document.hidden || interval) return;
		interval = setInterval(runPoll, intervalMs);
	}

	function handleVisibilityChange() {
		if (document.hidden) {
			clearPollInterval();
			return;
		}

		if (immediate) {
			runPoll();
		}
		startPollInterval();
	}

	return {
		start: () => {
			if (started) return;
			started = true;
			document.addEventListener('visibilitychange', handleVisibilityChange);
			if (immediate && !document.hidden) {
				runPoll();
			}
			startPollInterval();
		},
		stop: () => {
			if (!started) return;
			started = false;
			clearPollInterval();
			document.removeEventListener('visibilitychange', handleVisibilityChange);
		}
	};
}

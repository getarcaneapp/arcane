import { extractApiErrorMessage } from '#lib/utils/api.js';

/**
 * Owns the periodic refresh used while the table is sorted by live CPU or memory.
 * Polling pauses while the tab is hidden and resumes on the next visibility change.
 */
export class ContainerResourcePoller {
	error = $state<string | null>(null);

	private timer: ReturnType<typeof setTimeout> | null = null;
	private generation = 0;
	private refresh: (() => Promise<unknown>) | null = null;
	private readonly resume = () => {
		if (!this.refresh || document.hidden) return;
		this.clearTimer();
		void this.poll(this.generation);
	};

	constructor(private readonly intervalMs: number) {}

	start(refresh: () => Promise<unknown>): void {
		this.stop();
		this.refresh = refresh;
		this.generation += 1;
		this.timer = setTimeout(() => this.poll(this.generation), this.intervalMs);
		document.addEventListener('visibilitychange', this.resume);
	}

	stop(): void {
		if (!this.refresh) return;
		this.refresh = null;
		this.generation += 1;
		this.clearTimer();
		document.removeEventListener('visibilitychange', this.resume);
	}

	private clearTimer(): void {
		if (this.timer) clearTimeout(this.timer);
		this.timer = null;
	}

	private async poll(generation: number): Promise<void> {
		const refresh = this.refresh;
		if (!refresh || generation !== this.generation) return;
		if (document.hidden) {
			this.timer = setTimeout(() => this.poll(generation), this.intervalMs);
			return;
		}
		try {
			await refresh();
			if (generation === this.generation) this.error = null;
		} catch (err) {
			if (generation === this.generation) this.error = extractApiErrorMessage(err);
		}
		if (generation === this.generation) {
			this.timer = setTimeout(() => this.poll(generation), this.intervalMs);
		}
	}
}

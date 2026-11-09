import { m } from '#lib/paraglide/messages.js';
import { extractErrorMessage } from '#lib/utils/docker.js';
import { sanitizeLogText } from '#lib/utils/formatting.js';

export type BuildRunPhase = 'idle' | 'building' | 'completed' | 'failed';

export class BuildRun {
	logLines = $state<string[]>([]);
	statusText = $state('');
	error = $state('');
	isBuilding = $state(false);
	completed = $state(false);
	autoScroll = $state(true);

	phase = $derived.by<BuildRunPhase>(() => {
		if (this.error) return 'failed';
		if (this.completed) return 'completed';
		if (this.isBuilding) return 'building';
		return 'idle';
	});

	reset = () => {
		this.isBuilding = false;
		this.statusText = '';
		this.error = '';
		this.completed = false;
		this.logLines = [];
	};

	start(contextDir: string) {
		this.reset();
		this.isBuilding = true;
		this.statusText = m.starting_build();
		this.append(m.using_context({ context: contextDir }));
	}

	append(line: string) {
		if (sanitizeLogText(line).trim() === '') return;
		this.logLines = [...this.logLines, line];
	}

	fail(message: string) {
		const clean = sanitizeLogText(message);
		this.error = clean;
		this.statusText = clean.toLowerCase().startsWith(m.build_failed().toLowerCase())
			? clean
			: m.build_failed_with_error({ error: clean });
		this.append(m.build_error_log({ error: clean }));
	}

	complete() {
		this.completed = true;
		this.statusText = m.build_completed();
		this.append(m.build_completed());
	}

	handleLine = (line: string): boolean => {
		if (line.trim() === '') return false;
		try {
			const event = JSON.parse(line) as Record<string, unknown>;
			const errorMsg = extractErrorMessage(event, m.build_failed());
			if (errorMsg) {
				this.fail(errorMsg);
				return false;
			}
			if (event['done'] === true) return true;
			if (typeof event['log'] === 'string') {
				this.append(event['log']);
				this.statusText = sanitizeLogText(event['log']);
			}
		} catch {
			this.append(line);
		}
		return false;
	};
}

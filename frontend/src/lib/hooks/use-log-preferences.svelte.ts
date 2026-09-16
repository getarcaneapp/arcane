import { PersistedState } from 'runed';

export class UseLogPreferences {
	selectedTail = new PersistedState('arcane_log_tail_lines', '100');
	autoStart = new PersistedState('arcane_log_auto_start', 'false');
	parsedJson = new PersistedState('arcane_log_json_parsing_v3', 'false');
	streamLabels = new PersistedState('arcane_log_show_stream_labels', 'true');
	timestamps = new PersistedState('arcane_log_show_timestamps', 'true');

	get tailLines() {
		const selectedTail = this.selectedTail.current || '100';
		if (selectedTail === 'all') return 999999;
		return parseInt(selectedTail, 10);
	}

	get autoStartLogs() {
		return this.autoStart.current === 'true';
	}

	set autoStartLogs(enabled: boolean) {
		this.autoStart.current = String(enabled);
	}

	get showStreamLabels() {
		return this.streamLabels.current === 'true';
	}

	set showStreamLabels(enabled: boolean) {
		this.streamLabels.current = String(enabled);
	}

	get showTimestamps() {
		return this.timestamps.current === 'true';
	}

	set showTimestamps(enabled: boolean) {
		this.timestamps.current = String(enabled);
	}

	// The viewer may enable parsed mode for this session without persisting it.
	showParsedJson = $derived(this.parsedJson.current === 'true');

	setParsedMode(enabled: boolean) {
		this.parsedJson.current = String(enabled);
		this.showParsedJson = enabled;
	}
}

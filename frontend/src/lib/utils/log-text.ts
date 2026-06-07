const ANSI_ESCAPE_SEQUENCE = /\x1B\[[0-?]*[ -/]*[@-~]/g;
const ANSI_OSC_SEQUENCE = /\x1B\][^\x07]*(?:\x07|\x1B\\)/g;
const LOOSE_ANSI_MARKER_SEQUENCE = /\[(?:\d{1,3}(?:;\d{1,3})*)m/g;

export function stripAnsi(input: string): string {
	return input.replace(ANSI_ESCAPE_SEQUENCE, '').replace(ANSI_OSC_SEQUENCE, '').replace(LOOSE_ANSI_MARKER_SEQUENCE, '');
}

export function sanitizeLogText(input: string): string {
	return stripAnsi(input.replace(/\r/g, '')).trimEnd();
}

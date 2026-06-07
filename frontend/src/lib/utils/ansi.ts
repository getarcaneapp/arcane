import Convert from 'ansi-to-html';

const converter = new Convert({
	fg: '#e4e4e7',
	bg: '#000000',
	newline: false,
	escapeXML: true,
	stream: false,
	colors: {
		0: '#18181b',
		1: '#ef4444',
		2: '#22c55e',
		3: '#eab308',
		4: '#3b82f6',
		5: '#a855f7',
		6: '#06b6d4',
		7: '#f4f4f5',
		8: '#71717a',
		9: '#f87171',
		10: '#4ade80',
		11: '#facc15',
		12: '#60a5fa',
		13: '#c084fc',
		14: '#22d3ee',
		15: '#fafafa'
	}
});

export function ansiToHtml(text: string): string {
	if (!text) return '';
	return converter.toHtml(text);
}

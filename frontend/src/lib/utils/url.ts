export function toPortHref(hostPort: string, baseServerUrl?: string): string {
	try {
		const base = baseServerUrl || (typeof window !== 'undefined' ? window.location.origin : 'http://localhost');
		const url = new URL(base.startsWith('http') ? base : `http://${base}`);
		url.port = hostPort;
		return url.toString();
	} catch {
		return '#';
	}
}

export function toSafeHref(raw: string, scheme: string = 'https'): string {
	const trimmed = raw.trim();
	if (!trimmed) return '#';
	if (/^(javascript|data|vbscript):/i.test(trimmed)) return '#';
	if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(trimmed)) return trimmed;
	return `${scheme}://${trimmed}`;
}

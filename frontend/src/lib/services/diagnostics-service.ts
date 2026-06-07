import BaseAPIService from './api-service';
import type { Diagnostics, LogEntry, PprofProfile } from '$lib/types/diagnostics';

class DiagnosticsAPIService extends BaseAPIService {
	/** One-shot runtime/memory/GC + WebSocket snapshot (used for initial paint). */
	async getDiagnostics(): Promise<Diagnostics> {
		const res = await this.api.get('/diagnostics');
		return res.data as Diagnostics;
	}

	/** Recent buffered backend log entries (oldest first). */
	async getRecentLogs(): Promise<LogEntry[]> {
		const res = await this.api.get('/diagnostics/logs');
		return (res.data ?? []) as LogEntry[];
	}

	/** Fetch a human-readable pprof dump (debug=2 text) for inline display. */
	async getDump(name: 'goroutine' | 'heap'): Promise<string> {
		const res = await this.api.get(`/debug/pprof/${name}`, {
			params: { debug: 2 },
			responseType: 'text'
		});
		return (res.data ?? '') as string;
	}

	/**
	 * Download a raw pprof profile via the authed client (so bearer/API-key auth
	 * is attached) and trigger a browser save. `profile` and `trace` are
	 * time-sampled; pass an optional duration in seconds.
	 */
	async downloadProfile(profile: PprofProfile, seconds?: number): Promise<void> {
		const params: Record<string, number> = {};
		if (profile === 'profile') params['seconds'] = seconds ?? 30;
		if (profile === 'trace') params['seconds'] = seconds ?? 5;

		const defaultSeconds = profile === 'trace' ? 5 : 30;
		const res = await this.api.get(`/debug/pprof/${profile}`, {
			params,
			responseType: 'blob',
			// Sampled profiles block for their full duration.
			timeout: profile === 'profile' || profile === 'trace' ? (seconds ?? defaultSeconds) * 1000 + 15000 : undefined
		});

		const blob = res.data as Blob;
		const url = URL.createObjectURL(blob);
		const ext = profile === 'trace' ? 'out' : 'pprof';
		const anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = `${profile}.${ext}`;
		document.body.appendChild(anchor);
		anchor.click();
		anchor.remove();
		URL.revokeObjectURL(url);
	}
}

export const diagnosticsService = new DiagnosticsAPIService();

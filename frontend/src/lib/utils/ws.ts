import { m } from '#lib/paraglide/messages.js';
import { currentSessionSignal } from '#lib/services/api-service.js';
import type {
	ActorDiagnostics,
	Diagnostics,
	DiagnosticsCommand,
	DiagnosticsMessage,
	LogEntry,
	PprofProfile
} from '#lib/types/diagnostics.js';
import type { SystemStats } from '#lib/types/shared.js';
import { downloadBlob } from '#lib/utils/browser-download.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export interface ReconnectWSOptions<T> {
	buildUrl: () => string | Promise<string>;
	parseMessage?: (evt: MessageEvent) => T;
	onMessage?: (msg: T) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (err: Event | Error) => void;
	maxBackoff?: number;
	autoConnect?: boolean;
	shouldReconnect?: () => boolean;
}

export class ReconnectingWebSocket<T = unknown> {
	private ws: WebSocket | null = null;
	private closed = true;
	private attempt = 0;
	private readonly maxBackoff: number;
	private opts: ReconnectWSOptions<T>;
	private connecting = false;
	private generation = 0;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

	constructor(opts: ReconnectWSOptions<T>) {
		this.opts = opts;
		this.maxBackoff = opts.maxBackoff ?? 30000;
		if (opts.autoConnect) this.connect();
	}

	async connect() {
		this.close();
		this.closed = false;
		await this.connectOnce();
	}

	async connectOnce() {
		if (this.closed || this.connecting) return;

		if (this.ws) {
			try {
				this.ws.close();
			} catch {}
			this.ws = null;
		}

		const generation = ++this.generation;
		this.connecting = true;
		const urlResult = await tryCatch((async () => await this.opts.buildUrl())());
		if (generation !== this.generation || this.closed) return;
		if (urlResult.error !== null) {
			this.connecting = false;
			this.scheduleReconnect();
			this.opts.onError?.(urlResult.error);
			return;
		}
		const url = urlResult.data;

		let socket: WebSocket;
		try {
			socket = new WebSocket(url);
		} catch (err) {
			this.connecting = false;
			this.scheduleReconnect();
			this.opts.onError?.(err as Error);
			return;
		}

		this.ws = socket;

		socket.onopen = () => {
			if (socket !== this.ws) return;
			this.attempt = 0;
			this.connecting = false;
			this.opts.onOpen?.();
		};

		socket.onmessage = (evt) => {
			if (socket !== this.ws) return;
			try {
				const parser =
					this.opts.parseMessage ??
					((e: MessageEvent) => {
						if (typeof e.data === 'string') return JSON.parse(e.data) as unknown as T;
						return e.data as unknown as T;
					});
				const msg = parser(evt);
				this.opts.onMessage?.(msg);
			} catch (err) {
				this.opts.onError?.(err as Error);
			}
		};

		socket.onerror = (e) => {
			if (socket !== this.ws) return;
			this.opts.onError?.(e);
		};

		socket.onclose = () => {
			if (socket !== this.ws) return;
			this.opts.onClose?.();
			this.ws = null;
			this.connecting = false;
			if (!this.closed) this.scheduleReconnect();
		};

		return;
	}

	private scheduleReconnect() {
		if (currentSessionSignal().aborted || (this.opts.shouldReconnect && !this.opts.shouldReconnect())) {
			return;
		}

		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
		}

		this.attempt++;
		const exp = Math.min(1000 * Math.pow(1.5, this.attempt), this.maxBackoff);
		const jitter = Math.random() * 0.3 * exp;
		const backoff = exp - jitter;

		this.reconnectTimer = setTimeout(() => {
			this.reconnectTimer = null;
			if (!this.closed) this.connectOnce();
		}, backoff);
	}

	send(data: string): boolean {
		if (this.ws?.readyState !== WebSocket.OPEN) return false;
		this.ws.send(data);
		return true;
	}

	close() {
		this.closed = true;
		this.attempt = 0;
		this.generation++;

		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}

		try {
			this.ws?.close();
		} catch {}
		this.connecting = false;
	}

	closeAndWait(timeoutMs = 2000) {
		this.closed = true;
		this.attempt = 0;
		this.generation++;

		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}

		const socket = this.ws;
		if (!socket || socket.readyState === WebSocket.CLOSED) {
			this.ws = null;
			this.connecting = false;
			return Promise.resolve();
		}

		return new Promise<void>((resolve) => {
			let timeout: ReturnType<typeof setTimeout> | null = null;
			const finalize = () => {
				if (timeout) clearTimeout(timeout);
				socket.removeEventListener('close', handleClose);
				if (socket === this.ws) {
					this.ws = null;
				}
				this.connecting = false;
				resolve();
			};
			const handleClose = () => finalize();

			socket.addEventListener('close', handleClose, { once: true });
			try {
				socket.close();
			} catch {
				// Ignore close errors; rely on timeout/close event.
			}

			if (timeoutMs > 0) {
				timeout = setTimeout(finalize, timeoutMs);
			}
		});
	}
}

export function createStatsWebSocket(opts: {
	getEnvId: () => string;
	onMessage: (data: SystemStats) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (err: Event | Error) => void;
	maxBackoff?: number;
}) {
	const buildUrl = () => {
		const envId = opts.getEnvId() || '0';
		const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${location.host}/api/environments/${envId}/ws/system/stats`;
	};

	return new ReconnectingWebSocket<SystemStats>({
		buildUrl,
		parseMessage: (evt) => JSON.parse(evt.data as string) as SystemStats,
		onMessage: opts.onMessage,
		onOpen: opts.onOpen,
		onClose: opts.onClose,
		onError: opts.onError,
		maxBackoff: opts.maxBackoff
	});
}

export function createContainerStatsWebSocket(opts: {
	getEnvId: () => string;
	containerId: string;
	onMessage: (data: any) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (err: Event | Error) => void;
	maxBackoff?: number;
	shouldReconnect?: () => boolean;
}) {
	const buildUrl = () => {
		const envId = opts.getEnvId() || '0';
		const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${location.host}/api/environments/${envId}/ws/containers/${opts.containerId}/stats`;
	};

	return new ReconnectingWebSocket<any>({
		buildUrl,
		parseMessage: (evt) => JSON.parse(evt.data as string),
		onMessage: opts.onMessage,
		onOpen: opts.onOpen,
		onClose: opts.onClose,
		onError: opts.onError,
		maxBackoff: opts.maxBackoff,
		autoConnect: false,
		shouldReconnect: opts.shouldReconnect
	});
}

/** Diagnostics stream carrying live snapshots plus request/response commands. */
export function createDiagnosticsWebSocket(opts: {
	onSnapshot: (data: Diagnostics) => void;
	onOpen?: () => void;
	onClose?: () => void;
}) {
	let nextId = 0;
	const pending = new Map<string, { resolve: (msg: DiagnosticsMessage) => void; reject: (err: Error) => void }>();
	const rejectPending = () => {
		for (const entry of pending.values()) entry.reject(new Error(m.disconnected()));
		pending.clear();
	};

	const socket = createDiagnosticsStreamWebSocket<DiagnosticsMessage>('/api/diagnostics/stream', {
		onMessage: (msg) => {
			if (msg.type === 'snapshot') {
				if (msg.snapshot) opts.onSnapshot(msg.snapshot);
				return;
			}
			const entry = pending.get(msg.id ?? '');
			if (!entry) return;
			pending.delete(msg.id ?? '');
			if (msg.error) entry.reject(new Error(msg.error));
			else entry.resolve(msg);
		},
		onOpen: opts.onOpen,
		onClose: () => {
			rejectPending();
			opts.onClose?.();
		}
	});

	const request = (command: Omit<DiagnosticsCommand, 'id'>) =>
		new Promise<DiagnosticsMessage>((resolve, reject) => {
			const id = String(++nextId);
			if (!socket.send(JSON.stringify({ ...command, id }))) {
				reject(new Error(m.disconnected()));
				return;
			}
			pending.set(id, { resolve, reject });
		});

	return {
		connect: () => socket.connect(),
		close: () => {
			socket.close();
			rejectPending();
		},
		refresh: () => socket.send(JSON.stringify({ id: '', type: 'refresh' })),
		request,
		/** Captures a pprof profile over the stream and saves it as a file. */
		async downloadProfile(profile: PprofProfile) {
			const msg = await request({ type: 'profile', name: profile });
			const bytes = Uint8Array.from(atob(msg.data ?? ''), (char) => char.charCodeAt(0));
			downloadBlob(bytes, `${profile}.${profile === 'trace' ? 'out' : 'pprof'}`);
		}
	};
}

export type DiagnosticsWebSocket = ReturnType<typeof createDiagnosticsWebSocket>;

export function createActorDiagnosticsWebSocket(opts: {
	onMessage: (data: ActorDiagnostics[]) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (err: Event | Error) => void;
	maxBackoff?: number;
}) {
	return createDiagnosticsStreamWebSocket('/api/diagnostics/actors/stream', opts);
}

export function createBackendLogsWebSocket(opts: {
	onMessage: (data: LogEntry) => void;
	onOpen?: () => void;
	onClose?: () => void;
	onError?: (err: Event | Error) => void;
	maxBackoff?: number;
}) {
	return createDiagnosticsStreamWebSocket('/api/diagnostics/logs/stream', opts);
}

function createDiagnosticsStreamWebSocket<T>(
	path: string,
	opts: {
		onMessage: (data: T) => void;
		onOpen?: () => void;
		onClose?: () => void;
		onError?: (err: Event | Error) => void;
		maxBackoff?: number;
	}
): ReconnectingWebSocket<T> {
	const buildUrl = () => {
		const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
		return `${protocol}://${location.host}${path}`;
	};

	return new ReconnectingWebSocket<T>({
		buildUrl,
		parseMessage: (evt) => JSON.parse(evt.data as string) as T,
		onMessage: opts.onMessage,
		onOpen: opts.onOpen,
		onClose: opts.onClose,
		onError: opts.onError,
		maxBackoff: opts.maxBackoff
	});
}

// --- Debounce helper ---

export function debounced<T extends (...args: any[]) => void>(func: T, delay: number) {
	let debounceTimeout: ReturnType<typeof setTimeout>;

	return (...args: Parameters<T>) => {
		if (debounceTimeout !== undefined) {
			clearTimeout(debounceTimeout);
		}

		debounceTimeout = setTimeout(() => {
			func(...args);
		}, delay);
	};
}

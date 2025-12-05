import type { SystemStats } from '$lib/types/system-stats.type';

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
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

	constructor(opts: ReconnectWSOptions<T>) {
		this.opts = opts;
		this.maxBackoff = opts.maxBackoff ?? 30000;
		if (opts.autoConnect) this.connect();
	}

	async connect() {
		if (this.ws && !this.closed) {
			this.close();
		}
		this.closed = false;
		this.attempt = 0;
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

		this.connecting = true;
		let url: string;
		try {
			url = await this.opts.buildUrl();
		} catch (err) {
			this.connecting = false;
			this.scheduleReconnect();
			this.opts.onError?.(err as Error);
			return;
		}

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
		if (this.opts.shouldReconnect && !this.opts.shouldReconnect()) {
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

	send(payload: string | ArrayBuffer | Blob) {
		try {
			if (this.ws && this.ws.readyState === WebSocket.OPEN) {
				this.ws.send(payload as any);
				return true;
			}
		} catch (err) {
			this.opts.onError?.(err as Error);
		}
		return false;
	}

	close() {
		this.closed = true;
		this.attempt = 0;

		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}

		try {
			this.ws?.close();
		} catch {}
		this.connecting = false;
	}

	isConnected() {
		return !!this.ws && this.ws.readyState === WebSocket.OPEN;
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

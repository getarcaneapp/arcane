export type SSEEventBase = {
	type?: string;
};

export interface SSEStreamConfig<TEvent extends SSEEventBase> {
	/** Used in console warnings, e.g. 'Dashboard' / 'Activity' / 'Environment'. */
	label: string;
	openStream(signal: AbortSignal): Promise<Response>;
	/** Receives every event except 'heartbeat', which is connection state the transport owns. */
	onEvent(event: TEvent): void;
	/** Runs on each successful (re)connect, before any event is delivered. */
	onConnected?(): void;
}

export type SSEEventBase = {
	type?: string;
};

export interface SSEStreamConfig<TEvent extends SSEEventBase> {
	label: string;
	openStream(signal: AbortSignal): Promise<Response>;
	onEvent(event: TEvent): void;
	onConnected?(): void;
}

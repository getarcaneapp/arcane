export interface ApnsDevice {
	id: string;
	label: string;
	events: Record<string, boolean>;
	environmentIds: string[];
	createdAt: string;
	lastSeenAt?: string;
}

export interface ApnsStatus {
	enabled: boolean;
	channelId?: string;
	relayUrl: string;
	devices: ApnsDevice[];
}

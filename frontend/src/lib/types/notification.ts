export type NotificationProvider =
	| 'discord'
	| 'email'
	| 'telegram'
	| 'signal'
	| 'slack'
	| 'ntfy'
	| 'pushover'
	| 'gotify'
	| 'matrix'
	| 'googlechat'
	| 'generic';

export interface NotificationSettings {
	config: Record<string, unknown>;
	provider: NotificationProvider;
	id: number;
	enabled: boolean;
}

export interface NotificationTestResponse {
	message: string;
	warning?: string;
}

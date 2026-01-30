export type NotificationProvider =
	| 'discord'
	| 'email'
	| 'telegram'
	| 'signal'
	| 'slack'
	| 'ntfy'
	| 'pushover'
	| 'gotify'
	| 'generic';
export type EmailTLSMode = 'none' | 'starttls' | 'ssl';

export interface NotificationSettings {
	provider: NotificationProvider;
	enabled: boolean;
	config?: Record<string, any>;
}

export interface AppriseSettings {
	id?: number;
	apiUrl: string;
	enabled: boolean;
	imageUpdateTag: string;
	containerUpdateTag: string;
}

export interface TestNotificationResponse {
	success: boolean;
	message?: string;
	error?: string;
}

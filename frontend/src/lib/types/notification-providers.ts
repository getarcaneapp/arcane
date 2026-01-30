import type { NotificationSettings, AppriseSettings, EmailTLSMode } from './notification.type';

// Provider keys - this is the source of truth for all providers (alphabetically sorted)
export const NOTIFICATION_PROVIDER_KEYS = [
	'discord',
	'email',
	'generic',
	'gotify',
	'ntfy',
	'pushover',
	'signal',
	'slack',
	'telegram'
] as const;
export type NotificationProviderKey = (typeof NOTIFICATION_PROVIDER_KEYS)[number];

// Base form values that all providers share
export interface BaseProviderFormValues {
	enabled: boolean;
	eventImageUpdate: boolean;
	eventContainerUpdate: boolean;
}

// Provider-specific form value types
export interface DiscordFormValues extends BaseProviderFormValues {
	webhookId: string;
	token: string;
	username: string;
	avatarUrl: string;
}

export interface EmailFormValues extends BaseProviderFormValues {
	smtpHost: string;
	smtpPort: number;
	smtpUsername: string;
	smtpPassword: string;
	fromAddress: string;
	toAddresses: string;
	tlsMode: EmailTLSMode;
}

export interface TelegramFormValues extends BaseProviderFormValues {
	botToken: string;
	chatIds: string;
	preview: boolean;
	notification: boolean;
	title: string;
}

export interface SignalFormValues extends BaseProviderFormValues {
	host: string;
	port: number;
	user: string;
	password: string;
	token: string;
	source: string;
	recipients: string;
	disableTls: boolean;
}

export interface SlackFormValues extends BaseProviderFormValues {
	token: string;
	botName: string;
	icon: string;
	color: string;
	title: string;
	channel: string;
	threadTs: string;
}

export interface NtfyFormValues extends BaseProviderFormValues {
	host: string;
	port: number;
	topic: string;
	username: string;
	password: string;
	priority: string;
	tags: string;
	icon: string;
	cache: boolean;
	firebase: boolean;
	disableTlsVerification: boolean;
}

export interface PushoverFormValues extends BaseProviderFormValues {
	token: string;
	user: string;
	devices: string;
	priority: number;
	title: string;
}

export interface GotifyFormValues extends BaseProviderFormValues {
	host: string;
	port: number;
	token: string;
	path: string;
	priority: number;
	title: string;
	disableTls: boolean;
}

export interface GenericFormValues extends BaseProviderFormValues {
	webhookUrl: string;
	method: string;
	contentType: string;
	titleKey: string;
	messageKey: string;
	customHeaders: string;
}

export interface AppriseFormValues {
	enabled: boolean;
	apiUrl: string;
	imageUpdateTag: string;
	containerUpdateTag: string;
}

// Union type for all provider form values
export type ProviderFormValues =
	| DiscordFormValues
	| EmailFormValues
	| TelegramFormValues
	| SignalFormValues
	| SlackFormValues
	| NtfyFormValues
	| PushoverFormValues
	| GotifyFormValues
	| GenericFormValues;

// Map provider keys to their form value types
export type ProviderFormValuesMap = {
	discord: DiscordFormValues;
	email: EmailFormValues;
	telegram: TelegramFormValues;
	signal: SignalFormValues;
	slack: SlackFormValues;
	ntfy: NtfyFormValues;
	pushover: PushoverFormValues;
	gotify: GotifyFormValues;
	generic: GenericFormValues;
};

// Provider state with current values and saved baseline
export interface ProviderState<T> {
	current: T;
	baseline: T;
	hasChanges: boolean;
	isValid: boolean;
	errors: Partial<Record<keyof T, string>>;
}

// Helper to convert settings to form values
export function discordSettingsToFormValues(settings?: NotificationSettings): DiscordFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		webhookId: (cfg?.webhookId as string) || '',
		token: (cfg?.token as string) || '',
		username: (cfg?.username as string) || 'Arcane',
		avatarUrl: (cfg?.avatarUrl as string) || '',
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function emailSettingsToFormValues(settings?: NotificationSettings): EmailFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		smtpHost: (cfg?.smtpHost as string) || '',
		smtpPort: (cfg?.smtpPort as number) || 587,
		smtpUsername: (cfg?.smtpUsername as string) || '',
		smtpPassword: (cfg?.smtpPassword as string) || '',
		fromAddress: (cfg?.fromAddress as string) || '',
		toAddresses: Array.isArray(cfg?.toAddresses) ? (cfg.toAddresses as string[]).join(', ') : '',
		tlsMode: ((cfg?.tlsMode as string) || 'starttls') as EmailTLSMode,
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function telegramSettingsToFormValues(settings?: NotificationSettings): TelegramFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		botToken: (cfg?.botToken as string) || '',
		chatIds: Array.isArray(cfg?.chatIds) ? (cfg.chatIds as string[]).join(', ') : '',
		preview: (cfg?.preview as boolean) ?? true,
		notification: (cfg?.notification as boolean) ?? true,
		title: (cfg?.title as string) || '',
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function signalSettingsToFormValues(settings?: NotificationSettings): SignalFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		host: (cfg?.host as string) || 'localhost',
		port: (cfg?.port as number) || 8080,
		user: (cfg?.user as string) || '',
		password: (cfg?.password as string) || '',
		token: (cfg?.token as string) || '',
		source: (cfg?.source as string) || '',
		recipients: Array.isArray(cfg?.recipients) ? (cfg.recipients as string[]).join(', ') : '',
		disableTls: (cfg?.disableTls as boolean) ?? false,
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function slackSettingsToFormValues(settings?: NotificationSettings): SlackFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		token: (cfg?.token as string) || '',
		botName: (cfg?.botName as string) || 'Arcane',
		icon: (cfg?.icon as string) || '',
		color: (cfg?.color as string) || '',
		title: (cfg?.title as string) || '',
		channel: (cfg?.channel as string) || '',
		threadTs: (cfg?.threadTs as string) || '',
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function appriseSettingsToFormValues(settings?: AppriseSettings): AppriseFormValues {
	return {
		enabled: settings?.enabled ?? false,
		apiUrl: settings?.apiUrl || '',
		imageUpdateTag: settings?.imageUpdateTag || '',
		containerUpdateTag: settings?.containerUpdateTag || ''
	};
}

// Helper to convert form values back to API format
export function discordFormValuesToSettings(values: DiscordFormValues): NotificationSettings {
	return {
		provider: 'discord',
		enabled: values.enabled,
		config: {
			webhookId: values.webhookId,
			token: values.token,
			username: values.username,
			avatarUrl: values.avatarUrl,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function emailFormValuesToSettings(values: EmailFormValues): NotificationSettings {
	return {
		provider: 'email',
		enabled: values.enabled,
		config: {
			smtpHost: values.smtpHost,
			smtpPort: values.smtpPort,
			smtpUsername: values.smtpUsername,
			smtpPassword: values.smtpPassword,
			fromAddress: values.fromAddress,
			toAddresses: values.toAddresses
				.split(',')
				.map((addr) => addr.trim())
				.filter((addr) => addr.length > 0),
			tlsMode: values.tlsMode,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function telegramFormValuesToSettings(values: TelegramFormValues): NotificationSettings {
	return {
		provider: 'telegram',
		enabled: values.enabled,
		config: {
			botToken: values.botToken,
			chatIds: values.chatIds
				.split(',')
				.map((id) => id.trim())
				.filter((id) => id.length > 0),
			preview: values.preview,
			notification: values.notification,
			title: values.title,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function signalFormValuesToSettings(values: SignalFormValues): NotificationSettings {
	return {
		provider: 'signal',
		enabled: values.enabled,
		config: {
			host: values.host,
			port: values.port,
			user: values.user,
			password: values.password,
			token: values.token,
			source: values.source,
			recipients: values.recipients
				.split(',')
				.map((recipient) => recipient.trim())
				.filter((recipient) => recipient.length > 0),
			disableTls: values.disableTls,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function slackFormValuesToSettings(values: SlackFormValues): NotificationSettings {
	return {
		provider: 'slack',
		enabled: values.enabled,
		config: {
			token: values.token,
			botName: values.botName,
			icon: values.icon,
			color: values.color,
			title: values.title,
			channel: values.channel,
			threadTs: values.threadTs,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function ntfySettingsToFormValues(settings?: NotificationSettings): NtfyFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		host: (cfg?.host as string) || 'ntfy.sh',
		port: (cfg?.port as number) || 0,
		topic: (cfg?.topic as string) || '',
		username: (cfg?.username as string) || '',
		password: (cfg?.password as string) || '',
		priority: (cfg?.priority as string) || 'default',
		tags: Array.isArray(cfg?.tags) ? (cfg.tags as string[]).join(', ') : '',
		icon: (cfg?.icon as string) || '',
		cache: (cfg?.cache as boolean) ?? true,
		firebase: (cfg?.firebase as boolean) ?? true,
		disableTlsVerification: (cfg?.disableTlsVerification as boolean) ?? false,
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function pushoverSettingsToFormValues(settings?: NotificationSettings): PushoverFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		token: (cfg?.token as string) || '',
		user: (cfg?.user as string) || '',
		devices: Array.isArray(cfg?.devices) ? (cfg.devices as string[]).join(', ') : '',
		priority: Number(cfg?.priority ?? 0),
		title: (cfg?.title as string) || '',
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function gotifySettingsToFormValues(settings?: NotificationSettings): GotifyFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	return {
		enabled: settings?.enabled ?? false,
		host: (cfg?.host as string) || '',
		port: (cfg?.port as number) || 0,
		token: (cfg?.token as string) || '',
		path: (cfg?.path as string) || '',
		priority: Number(cfg?.priority ?? 0),
		title: (cfg?.title as string) || '',
		disableTls: (cfg?.disableTls as boolean) ?? false,
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function genericSettingsToFormValues(settings?: NotificationSettings): GenericFormValues {
	const cfg = (settings?.config ?? {}) as Record<string, unknown>;
	const events = (cfg?.events ?? {}) as Record<string, boolean>;
	const customHeaders = (cfg?.customHeaders ?? {}) as Record<string, string>;

	// Convert customHeaders object to string format (key1:value1, key2:value2)
	const customHeadersStr = Object.entries(customHeaders)
		.map(([key, value]) => `${key}:${value}`)
		.join(', ');

	return {
		enabled: settings?.enabled ?? false,
		webhookUrl: (cfg?.webhookUrl as string) || '',
		method: (cfg?.method as string) || 'POST',
		contentType: (cfg?.contentType as string) || 'application/json',
		titleKey: (cfg?.titleKey as string) || 'title',
		messageKey: (cfg?.messageKey as string) || 'message',
		customHeaders: customHeadersStr,
		eventImageUpdate: events?.image_update ?? true,
		eventContainerUpdate: events?.container_update ?? true
	};
}

export function ntfyFormValuesToSettings(values: NtfyFormValues): NotificationSettings {
	return {
		provider: 'ntfy',
		enabled: values.enabled,
		config: {
			host: values.host,
			port: values.port,
			topic: values.topic,
			username: values.username,
			password: values.password,
			priority: values.priority,
			tags: values.tags
				.split(',')
				.map((tag) => tag.trim())
				.filter((tag) => tag.length > 0),
			icon: values.icon,
			cache: values.cache,
			firebase: values.firebase,
			disableTlsVerification: values.disableTlsVerification,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function pushoverFormValuesToSettings(values: PushoverFormValues): NotificationSettings {
	return {
		provider: 'pushover',
		enabled: values.enabled,
		config: {
			token: values.token,
			user: values.user,
			devices: values.devices
				.split(',')
				.map((device) => device.trim())
				.filter((device) => device.length > 0),
			priority: values.priority,
			title: values.title,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function gotifyFormValuesToSettings(values: GotifyFormValues): NotificationSettings {
	return {
		provider: 'gotify',
		enabled: values.enabled,
		config: {
			host: values.host,
			port: values.port,
			token: values.token,
			path: values.path,
			priority: values.priority,
			title: values.title,
			disableTls: values.disableTls,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function genericFormValuesToSettings(values: GenericFormValues): NotificationSettings {
	// Parse customHeaders string (format: "key1:value1, key2:value2") into object
	const customHeaders: Record<string, string> = {};
	if (values.customHeaders) {
		const headerPairs = values.customHeaders
			.split(',')
			.map((h) => h.trim())
			.filter((h) => h.length > 0);
		for (const pair of headerPairs) {
			const [key, ...valueParts] = pair.split(':');
			if (key && valueParts.length > 0) {
				customHeaders[key.trim()] = valueParts.join(':').trim();
			}
		}
	}

	return {
		provider: 'generic',
		enabled: values.enabled,
		config: {
			webhookUrl: values.webhookUrl,
			method: values.method,
			contentType: values.contentType,
			titleKey: values.titleKey,
			messageKey: values.messageKey,
			customHeaders: customHeaders,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate
			}
		}
	};
}

export function appriseFormValuesToSettings(values: AppriseFormValues): AppriseSettings {
	return {
		enabled: values.enabled,
		apiUrl: values.apiUrl,
		imageUpdateTag: values.imageUpdateTag,
		containerUpdateTag: values.containerUpdateTag
	};
}

import type { NotificationSettings, AppriseSettings, EmailTLSMode } from './notification.type';

// Provider keys - this is the source of truth for all providers (alphabetically sorted)
export const NOTIFICATION_PROVIDER_KEYS = [
	'discord',
	'email',
	'generic',
	'gotify',
	'matrix',
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
	eventVulnerabilityFound: boolean;
	eventPruneReport: boolean;
	eventAutoHeal: boolean;
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
	title: string;
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

export interface MatrixFormValues extends BaseProviderFormValues {
	host: string;
	port: number;
	rooms: string;
	username: string;
	password: string;
	disableTlsVerification: boolean;
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
	| MatrixFormValues
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
	matrix: MatrixFormValues;
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

type ProviderConfig = Record<string, unknown>;
type ProviderEvents = Partial<
	Record<'image_update' | 'container_update' | 'vulnerability_found' | 'prune_report' | 'auto_heal', boolean>
>;

function getConfig(settings?: NotificationSettings): ProviderConfig {
	return (settings?.config ?? {}) as ProviderConfig;
}

function getEvents(config: ProviderConfig): ProviderEvents {
	return (config['events'] ?? {}) as ProviderEvents;
}

function getString(config: ProviderConfig, key: string, fallback = ''): string {
	return typeof config[key] === 'string' ? (config[key] as string) : fallback;
}

function getNumber(config: ProviderConfig, key: string, fallback = 0): number {
	return typeof config[key] === 'number' ? (config[key] as number) : fallback;
}

function getBoolean(config: ProviderConfig, key: string, fallback = false): boolean {
	return typeof config[key] === 'boolean' ? (config[key] as boolean) : fallback;
}

function getStringArray(config: ProviderConfig, key: string): string[] {
	const value = config[key];
	if (!Array.isArray(value)) return [];
	return value.filter((item): item is string => typeof item === 'string');
}

function getStringRecord(config: ProviderConfig, key: string): Record<string, string> {
	const value = config[key];
	if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
	return Object.fromEntries(
		Object.entries(value as Record<string, unknown>).filter(([, entryValue]) => typeof entryValue === 'string') as Array<
			[string, string]
		>
	);
}

function eventFlagsToFormValues(
	events: ProviderEvents
): Pick<
	BaseProviderFormValues,
	'eventImageUpdate' | 'eventContainerUpdate' | 'eventVulnerabilityFound' | 'eventPruneReport' | 'eventAutoHeal'
> {
	return {
		eventImageUpdate: events['image_update'] ?? true,
		eventContainerUpdate: events['container_update'] ?? true,
		eventVulnerabilityFound: events['vulnerability_found'] ?? true,
		eventPruneReport: events['prune_report'] ?? true,
		eventAutoHeal: events['auto_heal'] ?? true
	};
}

// Helper to convert settings to form values
export function discordSettingsToFormValues(settings?: NotificationSettings): DiscordFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		webhookId: getString(cfg, 'webhookId'),
		token: getString(cfg, 'token'),
		username: getString(cfg, 'username', 'Arcane'),
		avatarUrl: getString(cfg, 'avatarUrl'),
		...eventFlagsToFormValues(events)
	};
}

export function emailSettingsToFormValues(settings?: NotificationSettings): EmailFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		smtpHost: getString(cfg, 'smtpHost'),
		smtpPort: getNumber(cfg, 'smtpPort', 587),
		smtpUsername: getString(cfg, 'smtpUsername'),
		smtpPassword: getString(cfg, 'smtpPassword'),
		fromAddress: getString(cfg, 'fromAddress'),
		toAddresses: getStringArray(cfg, 'toAddresses').join(', '),
		tlsMode: getString(cfg, 'tlsMode', 'starttls') as EmailTLSMode,
		...eventFlagsToFormValues(events)
	};
}

export function telegramSettingsToFormValues(settings?: NotificationSettings): TelegramFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		botToken: getString(cfg, 'botToken'),
		chatIds: getStringArray(cfg, 'chatIds').join(', '),
		preview: getBoolean(cfg, 'preview', true),
		notification: getBoolean(cfg, 'notification', true),
		title: getString(cfg, 'title'),
		...eventFlagsToFormValues(events)
	};
}

export function signalSettingsToFormValues(settings?: NotificationSettings): SignalFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		host: getString(cfg, 'host', 'localhost'),
		port: getNumber(cfg, 'port', 8080),
		user: getString(cfg, 'user'),
		password: getString(cfg, 'password'),
		token: getString(cfg, 'token'),
		source: getString(cfg, 'source'),
		recipients: getStringArray(cfg, 'recipients').join(', '),
		disableTls: getBoolean(cfg, 'disableTls', false),
		...eventFlagsToFormValues(events)
	};
}

export function slackSettingsToFormValues(settings?: NotificationSettings): SlackFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		token: getString(cfg, 'token'),
		botName: getString(cfg, 'botName', 'Arcane'),
		icon: getString(cfg, 'icon'),
		color: getString(cfg, 'color'),
		title: getString(cfg, 'title'),
		channel: getString(cfg, 'channel'),
		threadTs: getString(cfg, 'threadTs'),
		...eventFlagsToFormValues(events)
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
			}
		}
	};
}

export function ntfySettingsToFormValues(settings?: NotificationSettings): NtfyFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		host: getString(cfg, 'host', 'ntfy.sh'),
		port: getNumber(cfg, 'port', 0),
		topic: getString(cfg, 'topic'),
		username: getString(cfg, 'username'),
		password: getString(cfg, 'password'),
		title: getString(cfg, 'title'),
		priority: getString(cfg, 'priority', 'default'),
		tags: getStringArray(cfg, 'tags').join(', '),
		icon: getString(cfg, 'icon'),
		cache: getBoolean(cfg, 'cache', true),
		firebase: getBoolean(cfg, 'firebase', true),
		disableTlsVerification: getBoolean(cfg, 'disableTlsVerification', false),
		...eventFlagsToFormValues(events)
	};
}

export function pushoverSettingsToFormValues(settings?: NotificationSettings): PushoverFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		token: getString(cfg, 'token'),
		user: getString(cfg, 'user'),
		devices: getStringArray(cfg, 'devices').join(', '),
		priority: Number(cfg['priority'] ?? 0),
		title: getString(cfg, 'title'),
		...eventFlagsToFormValues(events)
	};
}

export function gotifySettingsToFormValues(settings?: NotificationSettings): GotifyFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		host: getString(cfg, 'host'),
		port: getNumber(cfg, 'port', 0),
		token: getString(cfg, 'token'),
		path: getString(cfg, 'path'),
		priority: Number(cfg['priority'] ?? 0),
		title: getString(cfg, 'title'),
		disableTls: getBoolean(cfg, 'disableTls', false),
		...eventFlagsToFormValues(events)
	};
}

export function matrixSettingsToFormValues(settings?: NotificationSettings): MatrixFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	return {
		enabled: settings?.enabled ?? false,
		host: getString(cfg, 'host'),
		port: getNumber(cfg, 'port', 0),
		rooms: getString(cfg, 'rooms'),
		username: getString(cfg, 'username'),
		password: getString(cfg, 'password'),
		disableTlsVerification: getBoolean(cfg, 'disableTlsVerification', false),
		...eventFlagsToFormValues(events)
	};
}

export function genericSettingsToFormValues(settings?: NotificationSettings): GenericFormValues {
	const cfg = getConfig(settings);
	const events = getEvents(cfg);
	const customHeaders = getStringRecord(cfg, 'customHeaders');

	// Convert customHeaders object to string format (key1:value1, key2:value2)
	const customHeadersStr = Object.entries(customHeaders)
		.map(([key, value]) => `${key}:${value}`)
		.join(', ');

	return {
		enabled: settings?.enabled ?? false,
		webhookUrl: getString(cfg, 'webhookUrl'),
		method: getString(cfg, 'method', 'POST'),
		contentType: getString(cfg, 'contentType', 'application/json'),
		titleKey: getString(cfg, 'titleKey', 'title'),
		messageKey: getString(cfg, 'messageKey', 'message'),
		customHeaders: customHeadersStr,
		...eventFlagsToFormValues(events)
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
			title: values.title,
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
			}
		}
	};
}

export function matrixFormValuesToSettings(values: MatrixFormValues): NotificationSettings {
	return {
		provider: 'matrix',
		enabled: values.enabled,
		config: {
			host: values.host,
			port: values.port,
			rooms: values.rooms,
			username: values.username,
			password: values.password,
			disableTlsVerification: values.disableTlsVerification,
			events: {
				image_update: values.eventImageUpdate,
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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
				container_update: values.eventContainerUpdate,
				vulnerability_found: values.eventVulnerabilityFound,
				prune_report: values.eventPruneReport,
				auto_heal: values.eventAutoHeal
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

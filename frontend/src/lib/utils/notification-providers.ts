import * as m from '#lib/paraglide/messages.js';
import {
	NOTIFICATION_PROVIDER_KEYS,
	type NotificationProviderKey,
	type NotificationSettings,
	type ProviderFormValuesMap,
	discordSettingsToFormValues,
	emailSettingsToFormValues,
	telegramSettingsToFormValues,
	signalSettingsToFormValues,
	slackSettingsToFormValues,
	ntfySettingsToFormValues,
	pushoverSettingsToFormValues,
	gotifySettingsToFormValues,
	matrixSettingsToFormValues,
	googleChatSettingsToFormValues,
	genericSettingsToFormValues,
	discordFormValuesToSettings,
	emailFormValuesToSettings,
	telegramFormValuesToSettings,
	signalFormValuesToSettings,
	slackFormValuesToSettings,
	ntfyFormValuesToSettings,
	pushoverFormValuesToSettings,
	gotifyFormValuesToSettings,
	matrixFormValuesToSettings,
	googleChatFormValuesToSettings,
	genericFormValuesToSettings
} from '#lib/types/notifications.js';

export type NotificationSettingsByProvider = Record<NotificationProviderKey, NotificationSettings | null>;
export type NotificationProviderFormState = { [K in NotificationProviderKey]: ProviderFormValuesMap[K] };

export type NotificationProviderDefinition<K extends NotificationProviderKey> = {
	key: K;
	label: () => string;
	description: () => string;
	fromSettings: (settings?: NotificationSettings) => ProviderFormValuesMap[K];
	toSettings: (values: ProviderFormValuesMap[K]) => NotificationSettings;
};

type NotificationProviderDefinitions = {
	[K in NotificationProviderKey]: NotificationProviderDefinition<K>;
};

const notificationProviderDefinitions = {
	discord: {
		key: 'discord',
		label: m.notifications_discord_title,
		description: m.notifications_discord_description,
		fromSettings: discordSettingsToFormValues,
		toSettings: discordFormValuesToSettings
	},
	email: {
		key: 'email',
		label: m.common_email,
		description: m.notifications_email_description,
		fromSettings: emailSettingsToFormValues,
		toSettings: emailFormValuesToSettings
	},
	generic: {
		key: 'generic',
		label: m.notifications_generic_title,
		description: m.notifications_generic_description,
		fromSettings: genericSettingsToFormValues,
		toSettings: genericFormValuesToSettings
	},
	googlechat: {
		key: 'googlechat',
		label: m.notifications_googlechat_title,
		description: m.notifications_googlechat_description,
		fromSettings: googleChatSettingsToFormValues,
		toSettings: googleChatFormValuesToSettings
	},
	gotify: {
		key: 'gotify',
		label: m.notifications_gotify_title,
		description: m.notifications_gotify_description,
		fromSettings: gotifySettingsToFormValues,
		toSettings: gotifyFormValuesToSettings
	},
	matrix: {
		key: 'matrix',
		label: m.notifications_matrix_title,
		description: m.notifications_matrix_description,
		fromSettings: matrixSettingsToFormValues,
		toSettings: matrixFormValuesToSettings
	},
	ntfy: {
		key: 'ntfy',
		label: m.notifications_ntfy_title,
		description: m.notifications_ntfy_description,
		fromSettings: ntfySettingsToFormValues,
		toSettings: ntfyFormValuesToSettings
	},
	pushover: {
		key: 'pushover',
		label: m.notifications_pushover_title,
		description: m.notifications_pushover_description,
		fromSettings: pushoverSettingsToFormValues,
		toSettings: pushoverFormValuesToSettings
	},
	signal: {
		key: 'signal',
		label: m.notifications_signal_title,
		description: m.notifications_signal_description,
		fromSettings: signalSettingsToFormValues,
		toSettings: signalFormValuesToSettings
	},
	slack: {
		key: 'slack',
		label: m.notifications_slack_title,
		description: m.notifications_slack_description,
		fromSettings: slackSettingsToFormValues,
		toSettings: slackFormValuesToSettings
	},
	telegram: {
		key: 'telegram',
		label: m.notifications_telegram_title,
		description: m.notifications_telegram_description,
		fromSettings: telegramSettingsToFormValues,
		toSettings: telegramFormValuesToSettings
	}
} satisfies NotificationProviderDefinitions;

export function createNotificationSettingsByProvider(settings: NotificationSettings[] = []): NotificationSettingsByProvider {
	const byProvider = Object.fromEntries(
		NOTIFICATION_PROVIDER_KEYS.map((provider) => [provider, null])
	) as NotificationSettingsByProvider;
	for (const setting of settings) {
		if (NOTIFICATION_PROVIDER_KEYS.includes(setting.provider as NotificationProviderKey)) {
			byProvider[setting.provider as NotificationProviderKey] = setting;
		}
	}
	return byProvider;
}

export function createNotificationProviderFormState(
	settings: NotificationSettingsByProvider = createNotificationSettingsByProvider()
): NotificationProviderFormState {
	return Object.fromEntries(
		NOTIFICATION_PROVIDER_KEYS.map((provider) => [
			provider,
			getNotificationProviderDefinition(provider).fromSettings(settings[provider] ?? undefined)
		])
	) as NotificationProviderFormState;
}

export function cloneNotificationProviderFormState(state: NotificationProviderFormState): NotificationProviderFormState {
	return Object.fromEntries(
		NOTIFICATION_PROVIDER_KEYS.map((provider) => [provider, { ...state[provider] }])
	) as NotificationProviderFormState;
}

export function updateNotificationProviderFormState<K extends NotificationProviderKey>(
	state: NotificationProviderFormState,
	provider: K,
	values: ProviderFormValuesMap[K]
): NotificationProviderFormState {
	return { ...state, [provider]: { ...values } } as NotificationProviderFormState;
}

export function getNotificationProviderDefinition<K extends NotificationProviderKey>(
	provider: K
): NotificationProviderDefinition<K> {
	return notificationProviderDefinitions[provider] as unknown as NotificationProviderDefinition<K>;
}

export function notificationProviderFormValuesToSettings<K extends NotificationProviderKey>(
	provider: K,
	values: ProviderFormValuesMap[K]
): NotificationSettings {
	return getNotificationProviderDefinition(provider).toSettings(values);
}

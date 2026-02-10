<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { z } from 'zod/v4';
	import type {
		NotificationProviderKey,
		ProviderFormValuesMap,
		DiscordFormValues,
		EmailFormValues,
		TelegramFormValues,
		SignalFormValues,
		SlackFormValues,
		NtfyFormValues,
		PushoverFormValues,
		GotifyFormValues,
		MatrixFormValues,
		GenericFormValues
	} from '$lib/types/notification-providers';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';
	import DynamicProviderFormBuilder from './DynamicProviderFormBuilder.svelte';
	import NotificationProviderTestMenu from './NotificationProviderTestMenu.svelte';
	import type { NotificationProviderTestOption } from './NotificationProviderTestMenu.svelte';
	import { mapZodFieldErrors } from './provider-form-validation';
	import type { ProviderFormSchema } from './provider-form-schema';

	type AnyBuiltInValues = ProviderFormValuesMap[NotificationProviderKey];

	interface Props {
		provider: NotificationProviderKey;
		values: AnyBuiltInValues;
		disabled?: boolean;
		isTesting?: boolean;
		onTest?: (testType?: string) => void;
	}

	let { provider, values = $bindable(), disabled = false, isTesting = false, onTest }: Props = $props();

	const providerMeta: Record<NotificationProviderKey, { title: string; description: string; enabledLabel: string }> = {
		discord: {
			title: 'Discord',
			description: m.notifications_discord_description(),
			enabledLabel: m.notifications_discord_enabled_label()
		},
		email: {
			title: 'Email',
			description: m.notifications_email_description(),
			enabledLabel: m.notifications_email_enabled_label()
		},
		telegram: {
			title: 'Telegram',
			description: 'Send notifications via Telegram bot',
			enabledLabel: 'Enable Telegram Notifications'
		},
		signal: {
			title: m.notifications_signal_title(),
			description: m.notifications_signal_description(),
			enabledLabel: m.notifications_signal_enabled_label()
		},
		slack: {
			title: m.notifications_slack_title(),
			description: m.notifications_slack_description(),
			enabledLabel: m.notifications_slack_enabled_label()
		},
		ntfy: {
			title: 'Ntfy',
			description: m.notifications_ntfy_description(),
			enabledLabel: m.notifications_ntfy_enabled_label()
		},
		pushover: {
			title: 'Pushover',
			description: m.notifications_pushover_description(),
			enabledLabel: m.notifications_pushover_enabled_label()
		},
		gotify: {
			title: 'Gotify',
			description: m.notifications_gotify_description(),
			enabledLabel: m.notifications_gotify_enabled_label()
		},
		matrix: {
			title: 'Matrix',
			description: m.notifications_matrix_description(),
			enabledLabel: m.notifications_matrix_enabled_label()
		},
		generic: {
			title: m.notifications_generic_title(),
			description: m.notifications_generic_description(),
			enabledLabel: m.notifications_generic_enabled_label()
		}
	};

	const providerSchemas: Record<NotificationProviderKey, z.ZodTypeAny> = {
		discord: z
			.object({
				enabled: z.boolean(),
				webhookId: z.string(),
				token: z.string(),
				username: z.string(),
				avatarUrl: z.string(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.webhookId.trim()) {
					ctx.addIssue({ code: 'custom', message: 'Webhook ID is required when Discord is enabled', path: ['webhookId'] });
				}
				if (!d.token.trim()) {
					ctx.addIssue({ code: 'custom', message: 'Webhook Token is required when Discord is enabled', path: ['token'] });
				}
			}),
		email: z
			.object({
				enabled: z.boolean(),
				smtpHost: z.string(),
				smtpPort: z.coerce.number().int().min(1).max(65535),
				smtpUsername: z.string(),
				smtpPassword: z.string(),
				fromAddress: z.string(),
				toAddresses: z.string(),
				tlsMode: z.enum(['none', 'starttls', 'ssl']),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.smtpHost.trim()) {
					ctx.addIssue({ code: 'custom', message: 'SMTP host is required when email is enabled', path: ['smtpHost'] });
				}
				if (!d.fromAddress.trim()) {
					ctx.addIssue({ code: 'custom', message: 'From address is required when email is enabled', path: ['fromAddress'] });
				} else {
					const v = z.string().email().safeParse(d.fromAddress.trim());
					if (!v.success) {
						ctx.addIssue({ code: 'custom', message: 'Invalid email address format', path: ['fromAddress'] });
					}
				}
				if (!d.toAddresses.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'At least one recipient address is required when email is enabled',
						path: ['toAddresses']
					});
				} else {
					const addresses = d.toAddresses
						.split(',')
						.map((addr) => addr.trim())
						.filter((addr) => addr.length > 0);
					const invalid: string[] = [];
					addresses.forEach((addr) => {
						const v = z.string().email().safeParse(addr);
						if (!v.success) invalid.push(addr);
					});
					if (invalid.length > 0) {
						ctx.addIssue({
							code: 'custom',
							message: `Invalid email addresses: ${invalid.join(', ')}`,
							path: ['toAddresses']
						});
					}
				}
			}),
		telegram: z
			.object({
				enabled: z.boolean(),
				botToken: z.string(),
				chatIds: z.string(),
				preview: z.boolean(),
				notification: z.boolean(),
				title: z.string(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.botToken.trim()) {
					ctx.addIssue({ code: 'custom', message: 'Bot Token is required when Telegram is enabled', path: ['botToken'] });
				}
				if (!d.chatIds.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'At least one Chat ID is required when Telegram is enabled',
						path: ['chatIds']
					});
				}
			}),
		signal: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.number().min(1).max(65535),
				user: z.string(),
				password: z.string(),
				token: z.string(),
				source: z.string(),
				recipients: z.string(),
				disableTls: z.boolean(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;

				if (!d.host.trim()) {
					ctx.addIssue({ code: 'custom', message: m.notifications_signal_host_required(), path: ['host'] });
				}

				if (d.port < 1 || d.port > 65535) {
					ctx.addIssue({ code: 'custom', message: m.notifications_signal_port_invalid(), path: ['port'] });
				}

				if (!d.source.trim()) {
					ctx.addIssue({ code: 'custom', message: m.notifications_signal_source_required(), path: ['source'] });
				} else if (!d.source.startsWith('+')) {
					ctx.addIssue({ code: 'custom', message: m.notifications_signal_source_format(), path: ['source'] });
				}

				if (!d.recipients.trim()) {
					ctx.addIssue({ code: 'custom', message: m.notifications_signal_recipients_required(), path: ['recipients'] });
				}

				const hasBasicAuth = d.user.trim() && d.password.trim();
				const hasTokenAuth = d.token.trim();
				if (!hasBasicAuth && !hasTokenAuth) {
					ctx.addIssue({
						code: 'custom',
						message: m.notifications_signal_auth_required(),
						path: ['user']
					});
				}
				if (hasBasicAuth && hasTokenAuth) {
					ctx.addIssue({
						code: 'custom',
						message: m.notifications_signal_auth_conflict(),
						path: ['token']
					});
				}
			}),
		slack: z
			.object({
				enabled: z.boolean(),
				token: z.string(),
				botName: z.string(),
				icon: z.string(),
				color: z.string(),
				title: z.string(),
				channel: z.string(),
				threadTs: z.string(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.token.trim()) {
					ctx.addIssue({ code: 'custom', message: m.notifications_slack_token_required(), path: ['token'] });
				}
			}),
		ntfy: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.number().min(0).max(65535),
				topic: z.string(),
				username: z.string(),
				password: z.string(),
				priority: z.string(),
				tags: z.string(),
				icon: z.string(),
				cache: z.boolean(),
				firebase: z.boolean(),
				disableTlsVerification: z.boolean(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.topic.trim()) {
					ctx.addIssue({ code: 'custom', message: 'Topic is required when Ntfy is enabled', path: ['topic'] });
				}
				if (d.port > 0 && (d.port < 1 || d.port > 65535)) {
					ctx.addIssue({ code: 'custom', message: 'Port must be between 1 and 65535', path: ['port'] });
				}
			}),
		pushover: z
			.object({
				enabled: z.boolean(),
				token: z.string(),
				user: z.string(),
				devices: z.string(),
				priority: z.coerce.number().int().min(-2).max(2),
				title: z.string(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.token.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['token'] });
				}
				if (!d.user.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['user'] });
				}
			}),
		gotify: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.coerce.number().int().min(0).max(65535),
				token: z.string(),
				path: z.string(),
				priority: z.coerce.number().int(),
				title: z.string(),
				disableTls: z.boolean(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.host.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['host'] });
				}
				if (!d.token.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['token'] });
				}
			}),
		matrix: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.coerce.number().int().min(0).max(65535),
				rooms: z.string(),
				username: z.string(),
				password: z.string(),
				disableTlsVerification: z.boolean(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.host.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['host'] });
				}
			}),
		generic: z
			.object({
				enabled: z.boolean(),
				webhookUrl: z.string(),
				method: z.string(),
				contentType: z.string(),
				titleKey: z.string(),
				messageKey: z.string(),
				customHeaders: z.string(),
				eventImageUpdate: z.boolean(),
				eventContainerUpdate: z.boolean(),
				eventVulnerabilityFound: z.boolean(),
				eventPruneReport: z.boolean()
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.webhookUrl.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: 'Webhook URL is required when Generic Webhook is enabled',
						path: ['webhookUrl']
					});
				}
			})
	};

	const providerFormSchemas: { [K in NotificationProviderKey]: ProviderFormSchema<ProviderFormValuesMap[K]> } = {
		discord: [
			{
				kind: 'input',
				key: 'webhookId',
				id: 'discord-webhook-id',
				label: m.notifications_discord_webhook_id_label(),
				placeholder: m.notifications_discord_webhook_id_placeholder(),
				helpText: m.notifications_discord_webhook_id_help()
			},
			{
				kind: 'input',
				key: 'token',
				id: 'discord-token',
				label: m.notifications_discord_token_label(),
				placeholder: m.notifications_discord_token_placeholder(),
				helpText: m.notifications_discord_token_help(),
				inputType: 'password'
			},
			{
				kind: 'input',
				key: 'username',
				id: 'discord-username',
				label: m.notifications_discord_username_label(),
				placeholder: m.notifications_discord_username_placeholder(),
				helpText: m.notifications_discord_username_help()
			},
			{
				kind: 'input',
				key: 'avatarUrl',
				id: 'discord-avatar-url',
				label: m.notifications_discord_avatar_url_label(),
				placeholder: m.notifications_discord_avatar_url_placeholder(),
				helpText: m.notifications_discord_avatar_url_help()
			}
		],
		email: [
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'smtpHost',
						id: 'smtp-host',
						label: m.notifications_email_smtp_host_label(),
						placeholder: m.notifications_email_smtp_host_placeholder(),
						helpText: m.notifications_email_smtp_host_help()
					},
					{
						kind: 'input',
						key: 'smtpPort',
						id: 'smtp-port',
						label: m.notifications_email_smtp_port_label(),
						placeholder: m.notifications_email_smtp_port_placeholder(),
						helpText: m.notifications_email_smtp_port_help(),
						inputType: 'number'
					}
				]
			},
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'smtpUsername',
						id: 'smtp-username',
						label: m.notifications_email_username_label(),
						placeholder: m.notifications_email_username_placeholder(),
						helpText: m.notifications_email_username_help()
					},
					{
						kind: 'input',
						key: 'smtpPassword',
						id: 'smtp-password',
						label: m.notifications_email_password_label(),
						placeholder: m.notifications_email_password_placeholder(),
						helpText: m.notifications_email_password_help(),
						inputType: 'password',
						autocomplete: 'new-password'
					}
				]
			},
			{
				kind: 'input',
				key: 'fromAddress',
				id: 'from-address',
				label: m.notifications_email_from_address_label(),
				placeholder: m.notifications_email_from_address_placeholder(),
				helpText: m.notifications_email_from_address_help(),
				inputType: 'email'
			},
			{
				kind: 'textarea',
				key: 'toAddresses',
				id: 'to-addresses',
				label: m.notifications_email_to_addresses_label(),
				placeholder: m.notifications_email_to_addresses_placeholder(),
				helpText: m.notifications_email_to_addresses_help(),
				rows: 2
			},
			{
				kind: 'select',
				key: 'tlsMode',
				id: 'email-tls-mode',
				label: m.notifications_email_tls_mode_label(),
				placeholder: m.notifications_email_tls_mode_placeholder(),
				description: m.notifications_email_tls_mode_description(),
				options: [
					{ value: 'none', label: 'None' },
					{ value: 'starttls', label: 'StartTLS' },
					{ value: 'ssl', label: 'SSL/TLS' }
				]
			}
		],
		telegram: [
			{
				kind: 'input',
				key: 'botToken',
				id: 'telegram-bot-token',
				label: 'Bot Token',
				placeholder: '123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11',
				helpText: 'The bot token from @BotFather',
				inputType: 'password'
			},
			{
				kind: 'textarea',
				key: 'chatIds',
				id: 'telegram-chat-ids',
				label: 'Chat IDs',
				placeholder: '@channel, 123456789, @another_channel',
				helpText: 'Comma-separated list of chat IDs or @channel names',
				rows: 2
			},
			{
				kind: 'input',
				key: 'title',
				id: 'telegram-title',
				label: 'Title (Optional)',
				placeholder: 'Arcane Notifications',
				helpText: 'Custom title for notifications'
			},
			{
				kind: 'row',
				className: 'space-y-3',
				fields: [
					{
						kind: 'switch',
						key: 'preview',
						id: 'telegram-preview',
						label: 'Enable Link Previews',
						description: 'Show web page previews for URLs in messages'
					},
					{
						kind: 'switch',
						key: 'notification',
						id: 'telegram-notification',
						label: 'Enable Notification Sound',
						description: 'Play notification sound when messages are received'
					}
				]
			}
		],
		signal: [
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'host',
						id: 'signal-host',
						label: m.notifications_signal_host_label(),
						placeholder: m.notifications_signal_host_placeholder(),
						helpText: m.notifications_signal_host_help()
					},
					{
						kind: 'input',
						key: 'port',
						id: 'signal-port',
						label: m.notifications_signal_port_label(),
						placeholder: m.notifications_signal_port_placeholder(),
						helpText: m.notifications_signal_port_help(),
						inputType: 'number'
					}
				]
			},
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'user',
						id: 'signal-user',
						label: m.notifications_signal_user_label(),
						placeholder: m.notifications_signal_user_placeholder(),
						helpText: m.notifications_signal_user_help()
					},
					{
						kind: 'input',
						key: 'password',
						id: 'signal-password',
						label: m.notifications_signal_password_label(),
						placeholder: m.notifications_signal_password_placeholder(),
						helpText: m.notifications_signal_password_help(),
						inputType: 'password'
					}
				]
			},
			{
				kind: 'input',
				key: 'token',
				id: 'signal-token',
				label: m.notifications_signal_token_label(),
				placeholder: m.notifications_signal_token_placeholder(),
				helpText: m.notifications_signal_token_help(),
				inputType: 'password'
			},
			{
				kind: 'input',
				key: 'source',
				id: 'signal-source',
				label: m.notifications_signal_source_label(),
				placeholder: m.notifications_signal_source_placeholder(),
				helpText: m.notifications_signal_source_help()
			},
			{
				kind: 'textarea',
				key: 'recipients',
				id: 'signal-recipients',
				label: m.notifications_signal_recipients_label(),
				placeholder: m.notifications_signal_recipients_placeholder(),
				helpText: m.notifications_signal_recipients_help(),
				rows: 3
			},
			{
				kind: 'switch',
				key: 'disableTls',
				id: 'signal-disable-tls',
				label: m.notifications_signal_disable_tls_label(),
				description: m.notifications_signal_disable_tls_description()
			}
		],
		slack: [
			{
				kind: 'input',
				key: 'token',
				id: 'slack-token',
				label: m.notifications_slack_token_label(),
				placeholder: m.notifications_slack_token_placeholder(),
				helpText: m.notifications_slack_token_help(),
				inputType: 'password'
			},
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'botName',
						id: 'slack-bot-name',
						label: m.notifications_slack_bot_name_label(),
						placeholder: m.notifications_slack_bot_name_placeholder(),
						helpText: m.notifications_slack_bot_name_help()
					},
					{
						kind: 'input',
						key: 'channel',
						id: 'slack-channel',
						label: m.notifications_slack_channel_label(),
						placeholder: m.notifications_slack_channel_placeholder(),
						helpText: m.notifications_slack_channel_help()
					}
				]
			},
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'icon',
						id: 'slack-icon',
						label: m.notifications_slack_icon_label(),
						placeholder: m.notifications_slack_icon_placeholder(),
						helpText: m.notifications_slack_icon_help()
					},
					{
						kind: 'input',
						key: 'color',
						id: 'slack-color',
						label: m.notifications_slack_color_label(),
						placeholder: m.notifications_slack_color_placeholder(),
						helpText: m.notifications_slack_color_help()
					}
				]
			},
			{
				kind: 'row',
				className: 'grid grid-cols-2 gap-4',
				fields: [
					{
						kind: 'input',
						key: 'title',
						id: 'slack-title',
						label: m.notifications_slack_title_label(),
						placeholder: m.notifications_slack_title_placeholder(),
						helpText: m.notifications_slack_title_help()
					},
					{
						kind: 'input',
						key: 'threadTs',
						id: 'slack-thread-ts',
						label: m.notifications_slack_thread_ts_label(),
						placeholder: m.notifications_slack_thread_ts_placeholder(),
						helpText: m.notifications_slack_thread_ts_help()
					}
				]
			}
		],
		ntfy: [
			{
				kind: 'input',
				key: 'host',
				id: 'ntfy-host',
				label: m.notifications_ntfy_host_label(),
				placeholder: m.notifications_ntfy_host_placeholder(),
				helpText: m.notifications_ntfy_host_help()
			},
			{
				kind: 'input',
				key: 'port',
				id: 'ntfy-port',
				label: m.notifications_ntfy_port_label(),
				placeholder: m.notifications_ntfy_port_placeholder(),
				helpText: m.notifications_ntfy_port_help(),
				inputType: 'number'
			},
			{
				kind: 'input',
				key: 'topic',
				id: 'ntfy-topic',
				label: m.notifications_ntfy_topic_label(),
				placeholder: m.notifications_ntfy_topic_placeholder(),
				helpText: m.notifications_ntfy_topic_help()
			},
			{
				kind: 'input',
				key: 'username',
				id: 'ntfy-username',
				label: m.notifications_ntfy_username_label(),
				placeholder: m.notifications_ntfy_username_placeholder(),
				helpText: m.notifications_ntfy_username_help()
			},
			{
				kind: 'input',
				key: 'password',
				id: 'ntfy-password',
				label: m.notifications_ntfy_password_label(),
				placeholder: m.notifications_ntfy_password_placeholder(),
				helpText: m.notifications_ntfy_password_help(),
				inputType: 'password'
			},
			{
				kind: 'native-select',
				key: 'priority',
				id: 'ntfy-priority',
				label: m.notifications_ntfy_priority_label(),
				description: m.notifications_ntfy_priority_help(),
				options: [
					{ value: 'min', label: 'Min (1)' },
					{ value: 'low', label: 'Low (2)' },
					{ value: 'default', label: 'Default (3)' },
					{ value: 'high', label: 'High (4)' },
					{ value: 'max', label: 'Max/Urgent (5)' }
				]
			},
			{
				kind: 'textarea',
				key: 'tags',
				id: 'ntfy-tags',
				label: m.notifications_ntfy_tags_label(),
				placeholder: m.notifications_ntfy_tags_placeholder(),
				helpText: m.notifications_ntfy_tags_help(),
				rows: 2
			},
			{
				kind: 'input',
				key: 'icon',
				id: 'ntfy-icon',
				label: m.notifications_ntfy_icon_label(),
				placeholder: m.notifications_ntfy_icon_placeholder(),
				helpText: m.notifications_ntfy_icon_help()
			},
			{
				kind: 'row',
				className: 'space-y-3',
				fields: [
					{
						kind: 'switch',
						key: 'cache',
						id: 'ntfy-cache',
						label: m.notifications_ntfy_cache_label(),
						description: m.notifications_ntfy_cache_help()
					},
					{
						kind: 'switch',
						key: 'firebase',
						id: 'ntfy-firebase',
						label: m.notifications_ntfy_firebase_label(),
						description: m.notifications_ntfy_firebase_help()
					},
					{
						kind: 'switch',
						key: 'disableTlsVerification',
						id: 'ntfy-disable-tls',
						label: m.notifications_ntfy_disable_tls_label(),
						description: m.notifications_ntfy_disable_tls_help()
					}
				]
			}
		],
		pushover: [
			{
				kind: 'input',
				key: 'token',
				id: 'pushover-token',
				label: m.notifications_pushover_token_label(),
				placeholder: m.notifications_pushover_token_placeholder(),
				helpText: m.notifications_pushover_token_help(),
				inputType: 'password'
			},
			{
				kind: 'input',
				key: 'user',
				id: 'pushover-user',
				label: m.notifications_pushover_user_label(),
				placeholder: m.notifications_pushover_user_placeholder(),
				helpText: m.notifications_pushover_user_help()
			},
			{
				kind: 'textarea',
				key: 'devices',
				id: 'pushover-devices',
				label: m.notifications_pushover_devices_label(),
				placeholder: m.notifications_pushover_devices_placeholder(),
				helpText: m.notifications_pushover_devices_help(),
				rows: 2
			},
			{
				kind: 'select',
				key: 'priority',
				id: 'pushover-priority',
				label: m.notifications_pushover_priority_label(),
				description: m.notifications_pushover_priority_help(),
				valueType: 'number',
				options: [
					{ value: '-2', label: '-2' },
					{ value: '-1', label: '-1' },
					{ value: '0', label: '0' },
					{ value: '1', label: '1' },
					{ value: '2', label: '2' }
				]
			},
			{
				kind: 'input',
				key: 'title',
				id: 'pushover-title',
				label: m.notifications_pushover_title_label(),
				placeholder: m.notifications_pushover_title_placeholder(),
				helpText: m.notifications_pushover_title_help()
			}
		],
		gotify: [
			{
				kind: 'row',
				className: 'grid grid-cols-1 gap-4 md:grid-cols-4',
				fields: [
					{
						kind: 'input',
						key: 'host',
						id: 'gotify-host',
						label: m.notifications_gotify_host_label(),
						placeholder: m.notifications_gotify_host_placeholder(),
						helpText: m.notifications_gotify_host_help(),
						wrapperClass: 'md:col-span-3'
					},
					{
						kind: 'input',
						key: 'port',
						id: 'gotify-port',
						label: m.notifications_gotify_port_label(),
						placeholder: m.notifications_gotify_port_placeholder(),
						helpText: m.notifications_gotify_port_help(),
						inputType: 'number',
						wrapperClass: 'md:col-span-1'
					}
				]
			},
			{
				kind: 'input',
				key: 'token',
				id: 'gotify-token',
				label: m.notifications_gotify_token_label(),
				placeholder: m.notifications_gotify_token_placeholder(),
				helpText: m.notifications_gotify_token_help(),
				inputType: 'password'
			},
			{
				kind: 'input',
				key: 'path',
				id: 'gotify-path',
				label: m.notifications_gotify_path_label(),
				placeholder: m.notifications_gotify_path_placeholder(),
				helpText: m.notifications_gotify_path_help()
			},
			{
				kind: 'select',
				key: 'priority',
				id: 'gotify-priority',
				label: m.notifications_gotify_priority_label(),
				description: m.notifications_gotify_priority_help(),
				valueType: 'number',
				options: [
					{ value: '-2', label: '-2 (Min)' },
					{ value: '-1', label: '-1 (Low)' },
					{ value: '0', label: '0 (None)' },
					{ value: '1', label: '1 (Low)' },
					{ value: '2', label: '2' },
					{ value: '3', label: '3' },
					{ value: '4', label: '4 (Normal)' },
					{ value: '5', label: '5' },
					{ value: '6', label: '6' },
					{ value: '7', label: '7 (High)' },
					{ value: '8', label: '8' },
					{ value: '9', label: '9' },
					{ value: '10', label: '10 (Max)' }
				]
			},
			{
				kind: 'input',
				key: 'title',
				id: 'gotify-title',
				label: m.notifications_gotify_title_label(),
				placeholder: m.notifications_gotify_title_placeholder(),
				helpText: m.notifications_gotify_title_help()
			},
			{
				kind: 'switch',
				key: 'disableTls',
				id: 'gotify-disable-tls',
				label: m.notifications_gotify_disable_tls_label(),
				description: m.notifications_gotify_disable_tls_help()
			}
		],
		matrix: [
			{
				kind: 'row',
				className: 'grid grid-cols-1 gap-4 md:grid-cols-4',
				fields: [
					{
						kind: 'input',
						key: 'host',
						id: 'matrix-host',
						label: m.notifications_matrix_host_label(),
						placeholder: m.notifications_matrix_host_placeholder(),
						helpText: m.notifications_matrix_host_help(),
						wrapperClass: 'md:col-span-3'
					},
					{
						kind: 'input',
						key: 'port',
						id: 'matrix-port',
						label: m.notifications_matrix_port_label(),
						placeholder: m.notifications_matrix_port_placeholder(),
						helpText: m.notifications_matrix_port_help(),
						inputType: 'number',
						wrapperClass: 'md:col-span-1'
					}
				]
			},
			{
				kind: 'input',
				key: 'rooms',
				id: 'matrix-rooms',
				label: m.notifications_matrix_rooms_label(),
				placeholder: m.notifications_matrix_rooms_placeholder(),
				helpText: m.notifications_matrix_rooms_help()
			},
			{
				kind: 'input',
				key: 'username',
				id: 'matrix-username',
				label: m.notifications_matrix_username_label(),
				placeholder: m.notifications_matrix_username_placeholder(),
				helpText: m.notifications_matrix_username_help()
			},
			{
				kind: 'input',
				key: 'password',
				id: 'matrix-password',
				label: m.notifications_matrix_password_label(),
				placeholder: m.notifications_matrix_password_placeholder(),
				helpText: m.notifications_matrix_password_help(),
				inputType: 'password'
			},
			{
				kind: 'switch',
				key: 'disableTlsVerification',
				id: 'matrix-disable-tls',
				label: m.notifications_matrix_disable_tls_label(),
				description: m.notifications_matrix_disable_tls_help()
			}
		],
		generic: [
			{
				kind: 'input',
				key: 'webhookUrl',
				id: 'generic-webhook-url',
				label: m.notifications_generic_webhook_url_label(),
				placeholder: m.notifications_generic_webhook_url_placeholder(),
				helpText: m.notifications_generic_webhook_url_help()
			},
			{
				kind: 'input',
				key: 'method',
				id: 'generic-method',
				label: m.notifications_generic_method_label(),
				placeholder: m.notifications_generic_method_placeholder(),
				helpText: m.notifications_generic_method_help()
			},
			{
				kind: 'input',
				key: 'contentType',
				id: 'generic-content-type',
				label: m.notifications_generic_content_type_label(),
				placeholder: m.notifications_generic_content_type_placeholder(),
				helpText: m.notifications_generic_content_type_help()
			},
			{
				kind: 'input',
				key: 'titleKey',
				id: 'generic-title-key',
				label: m.notifications_generic_title_key_label(),
				placeholder: m.notifications_generic_title_key_placeholder(),
				helpText: m.notifications_generic_title_key_help()
			},
			{
				kind: 'input',
				key: 'messageKey',
				id: 'generic-message-key',
				label: m.notifications_generic_message_key_label(),
				placeholder: m.notifications_generic_message_key_placeholder(),
				helpText: m.notifications_generic_message_key_help()
			},
			{
				kind: 'input',
				key: 'customHeaders',
				id: 'generic-custom-headers',
				label: m.notifications_generic_custom_headers_label(),
				placeholder: m.notifications_generic_custom_headers_placeholder(),
				helpText: m.notifications_generic_custom_headers_help()
			}
		]
	};

	const testOptions: NotificationProviderTestOption[] = [
		{ label: m.notifications_email_test_simple(), testType: 'simple' },
		{ label: m.notifications_email_test_image_update(), testType: 'image-update' },
		{ label: m.notifications_email_test_batch_image_update(), testType: 'batch-image-update' },
		{ label: m.notifications_test_vulnerability_notification(), testType: 'vulnerability-found' },
		{ label: m.notifications_test_prune_report_notification(), testType: 'prune-report' }
	];

	const validation = $derived.by(() => providerSchemas[provider].safeParse(values));
	const fieldErrors = $derived.by(() =>
		mapZodFieldErrors<AnyBuiltInValues>(validation as z.ZodSafeParseResult<AnyBuiltInValues>)
	);
	const selectedSchema = $derived(providerFormSchemas[provider] as ProviderFormSchema<AnyBuiltInValues>);
	const selectedMeta = $derived(providerMeta[provider]);

	export function isValid(): boolean {
		return validation.success;
	}
</script>

<ProviderFormWrapper
	id={provider}
	title={selectedMeta.title}
	description={selectedMeta.description}
	enabledLabel={selectedMeta.enabledLabel}
	bind:enabled={values.enabled}
	{disabled}
>
	<DynamicProviderFormBuilder bind:values {disabled} errors={fieldErrors} schema={selectedSchema} />

	<EventSubscriptions
		providerId={provider}
		bind:eventImageUpdate={values.eventImageUpdate}
		bind:eventContainerUpdate={values.eventContainerUpdate}
		bind:eventVulnerabilityFound={values.eventVulnerabilityFound}
		bind:eventPruneReport={values.eventPruneReport}
		{disabled}
	/>

	<NotificationProviderTestMenu {disabled} {isTesting} {onTest} options={testOptions} />
</ProviderFormWrapper>

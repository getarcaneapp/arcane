<script lang="ts">
	import { m } from '#lib/paraglide/messages';
	import { z } from 'zod/v4';
	import type { NotificationProviderKey, ProviderFormValuesMap } from '#lib/types/notifications';
	import ProviderFormWrapper from './ProviderFormWrapper.svelte';
	import EventSubscriptions from './EventSubscriptions.svelte';
	import DynamicProviderFormBuilder from './DynamicProviderFormBuilder.svelte';
	import NotificationProviderTestMenu, {
		type NotificationProviderTestOption,
		getDefaultNotificationProviderTestOptions
	} from './NotificationProviderTestMenu.svelte';
	import { mapZodFieldErrors } from './provider-form-validation';
	import type { ProviderFormSchema } from './provider-form-schema';
	import { getNotificationProviderDefinition } from '#lib/utils/notification-providers';

	type AnyBuiltInValues = ProviderFormValuesMap[NotificationProviderKey];

	// Go template syntax is deliberately kept out of the translatable messages:
	// the `{{ }}` delimiters collide with the message compiler's own placeholder
	// syntax, and the variable names are code rather than prose to translate.
	const GENERIC_PAYLOAD_TEMPLATE_PLACEHOLDER = '{"text": "{{.message}}"}';
	const GENERIC_PAYLOAD_TEMPLATE_VARS =
		'{{.title}}, {{.message}}, {{.environment}}, {{.environmentId}}, {{.event}}, {{.timestamp}}';

	interface Props {
		provider: NotificationProviderKey;
		values: AnyBuiltInValues;
		disabled?: boolean;
		isTesting?: boolean;
		hasExistingCredentials?: boolean;
		hasExistingPassword?: boolean;
		hasExistingToken?: boolean;
		onTest?: (testType?: string) => void;
	}

	let {
		provider,
		values = $bindable(),
		disabled = false,
		isTesting = false,
		hasExistingCredentials = false,
		hasExistingPassword = false,
		hasExistingToken = false,
		onTest
	}: Props = $props();

	const eventSubscriptionSchemaFields = {
		eventImageUpdate: z.boolean(),
		eventContainerUpdate: z.boolean(),
		eventVulnerabilityFound: z.boolean(),
		eventPruneReport: z.boolean(),
		eventAutoHeal: z.boolean()
	};

	function addCustomFieldIssue(ctx: z.RefinementCtx, path: string, message: string) {
		ctx.addIssue({ code: 'custom', message, path: [path] });
	}

	function addRequiredTrimmedFieldIssue(ctx: z.RefinementCtx, value: string, path: string, message: string) {
		if (!value.trim()) {
			addCustomFieldIssue(ctx, path, message);
		}
	}

	function addRequiredCredentialIssue(ctx: z.RefinementCtx, value: string, path: string, message: string) {
		if (!value.trim() && !hasExistingCredentials) {
			addCustomFieldIssue(ctx, path, message);
		}
	}

	const providerSchemas: Record<NotificationProviderKey, z.ZodTypeAny> = {
		discord: z
			.object({
				enabled: z.boolean(),
				webhookId: z.string(),
				token: z.string(),
				username: z.string(),
				avatarUrl: z.string(),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.webhookId, 'webhookId', m.common_required());
				addRequiredCredentialIssue(ctx, d.token, 'token', m.common_required());
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
				authMode: z.enum(['none', 'auto', 'plain', 'login', 'crammd5']),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				if (!d.smtpHost.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['smtpHost'] });
				}
				if (!d.fromAddress.trim()) {
					ctx.addIssue({ code: 'custom', message: m.common_required(), path: ['fromAddress'] });
				} else {
					const v = z.string().email().safeParse(d.fromAddress.trim());
					if (!v.success) {
						ctx.addIssue({ code: 'custom', message: m.common_invalid_email(), path: ['fromAddress'] });
					}
				}
				if (!d.toAddresses.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: m.common_required(),
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
							message: m.notifications_email_invalid_addresses({ addresses: invalid.join(', ') }),
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
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredCredentialIssue(ctx, d.botToken, 'botToken', m.common_required());
				if (!d.chatIds.trim()) {
					ctx.addIssue({
						code: 'custom',
						message: m.common_required(),
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
				...eventSubscriptionSchemaFields
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

				const wantsBasicAuth = Boolean(d.user.trim() || d.password.trim());
				const hasBasicAuth = Boolean(d.user.trim() && (d.password.trim() || hasExistingPassword));
				const hasTokenAuth = Boolean(d.token.trim() || (!wantsBasicAuth && hasExistingToken));
				if (d.user.trim() && !d.password.trim() && !hasExistingPassword) {
					ctx.addIssue({
						code: 'custom',
						message: m.notifications_signal_auth_required(),
						path: ['password']
					});
				}
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
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredCredentialIssue(ctx, d.token, 'token', m.notifications_slack_token_required());
			}),
		ntfy: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.number().min(0).max(65535),
				topic: z.string(),
				username: z.string(),
				password: z.string(),
				title: z.string(),
				priority: z.string(),
				tags: z.string(),
				icon: z.string(),
				cache: z.boolean(),
				firebase: z.boolean(),
				disableTls: z.boolean(),
				disableTlsVerification: z.boolean(),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.topic, 'topic', m.common_required());
				if (d.port > 0 && (d.port < 1 || d.port > 65535)) {
					addCustomFieldIssue(ctx, 'port', m.notifications_signal_port_invalid());
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
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredCredentialIssue(ctx, d.token, 'token', m.common_required());
				addRequiredTrimmedFieldIssue(ctx, d.user, 'user', m.common_required());
			}),
		gotify: z
			.object({
				enabled: z.boolean(),
				host: z.string(),
				port: z.coerce.number().int().min(0).max(65535),
				token: z.string(),
				path: z.string(),
				priority: z.coerce.number().int().min(-2).max(10),
				title: z.string(),
				disableTls: z.boolean(),
				insecureSkipVerify: z.boolean(),
				useHeader: z.boolean(),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.host, 'host', m.common_required());
				addRequiredCredentialIssue(ctx, d.token, 'token', m.common_required());
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
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.host, 'host', m.common_required());
			}),
		googlechat: z
			.object({
				enabled: z.boolean(),
				webhookUrl: z.string(),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.webhookUrl, 'webhookUrl', m.common_required());
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
				payloadTemplate: z.string(),
				successBodyContains: z.string(),
				...eventSubscriptionSchemaFields
			})
			.superRefine((d, ctx) => {
				if (!d.enabled) return;
				addRequiredTrimmedFieldIssue(ctx, d.webhookUrl, 'webhookUrl', m.common_required());
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
				placeholder: m.arcane_placeholder(),
				helpText: m.notifications_discord_username_help()
			},
			{
				kind: 'input',
				key: 'avatarUrl',
				id: 'discord-avatar-url',
				label: m.notifications_discord_avatar_url_label(),
				placeholder: m.https_placeholder(),
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
						placeholder: m.user_example_com_placeholder(),
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
					{ value: 'none', label: m.notifications_email_tls_none() },
					{ value: 'starttls', label: m.notifications_email_tls_starttls() },
					{ value: 'ssl', label: m.notifications_email_tls_ssl() }
				]
			},
			{
				kind: 'select',
				key: 'authMode',
				id: 'email-auth-mode',
				label: m.notifications_email_auth_mode_label(),
				placeholder: m.notifications_email_auth_mode_placeholder(),
				description: m.notifications_email_auth_mode_description(),
				options: [
					{ value: 'auto', label: m.notifications_email_auth_mode_option_auto() },
					{ value: 'none', label: m.notifications_email_auth_mode_option_none() },
					{ value: 'plain', label: m.notifications_email_auth_plain() },
					{ value: 'login', label: m.notifications_email_auth_login() },
					{ value: 'crammd5', label: m.notifications_email_auth_cram_md5() }
				]
			}
		],
		telegram: [
			{
				kind: 'input',
				key: 'botToken',
				id: 'telegram-bot-token',
				label: m.notifications_telegram_bot_token_label(),
				placeholder: m.notifications_telegram_bot_token_placeholder(),
				helpText: m.notifications_telegram_bot_token_help(),
				inputType: 'password'
			},
			{
				kind: 'textarea',
				key: 'chatIds',
				id: 'telegram-chat-ids',
				label: m.notifications_telegram_chat_ids_label(),
				placeholder: m.notifications_telegram_chat_ids_placeholder(),
				helpText: m.notifications_telegram_chat_ids_help(),
				rows: 2
			},
			{
				kind: 'input',
				key: 'title',
				id: 'telegram-title',
				label: m.notifications_telegram_title_label(),
				placeholder: m.notifications_telegram_title_placeholder(),
				helpText: m.notifications_telegram_title_help()
			},
			{
				kind: 'row',
				className: 'space-y-3',
				fields: [
					{
						kind: 'switch',
						key: 'preview',
						id: 'telegram-preview',
						label: m.notifications_telegram_preview_label(),
						description: m.notifications_telegram_preview_description()
					},
					{
						kind: 'switch',
						key: 'notification',
						id: 'telegram-notification',
						label: m.notifications_telegram_sound_label(),
						description: m.notifications_telegram_sound_description()
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
				label: m.disable_tls(),
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
						placeholder: m.arcane_placeholder(),
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
						placeholder: m.container_update_placeholder(),
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
				label: m.host(),
				placeholder: m.notifications_ntfy_host_placeholder(),
				helpText: m.notifications_ntfy_host_help()
			},
			{
				kind: 'input',
				key: 'port',
				id: 'ntfy-port',
				label: m.port_optional(),
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
				placeholder: m.username_placeholder(),
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
				kind: 'input',
				key: 'title',
				id: 'ntfy-title',
				label: m.title_optional(),
				placeholder: m.container_update_placeholder(),
				helpText: m.optional_title_override_for_notifications()
			},
			{
				kind: 'native-select',
				key: 'priority',
				id: 'ntfy-priority',
				label: m.priority(),
				description: m.notifications_ntfy_priority_help(),
				options: [
					{ value: 'min', label: `${m.notifications_priority_min()} (1)` },
					{ value: 'low', label: `${m.notifications_priority_low()} (2)` },
					{ value: 'default', label: `${m.notifications_priority_default()} (3)` },
					{ value: 'high', label: `${m.notifications_priority_high()} (4)` },
					{ value: 'max', label: `${m.notifications_priority_max_urgent()} (5)` }
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
				placeholder: m.https_placeholder(),
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
						key: 'disableTls',
						id: 'ntfy-use-http',
						label: m.notifications_ntfy_use_http_label(),
						description: m.notifications_ntfy_use_http_help()
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
				label: m.priority(),
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
				label: m.title_optional(),
				placeholder: m.container_update_placeholder(),
				helpText: m.optional_title_override_for_notifications()
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
						label: m.server_host(),
						placeholder: m.notifications_gotify_host_placeholder(),
						helpText: m.notifications_gotify_host_help(),
						wrapperClass: 'md:col-span-3'
					},
					{
						kind: 'input',
						key: 'port',
						id: 'gotify-port',
						label: m.port_optional(),
						placeholder: m.notifications_gotify_port_placeholder(),
						helpText: m.server_port_leave_at_0_to_use_default_443_for_https_80_for_http(),
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
				label: m.priority(),
				description: m.notifications_gotify_priority_help(),
				valueType: 'number',
				options: [
					{ value: '-2', label: `-2 (${m.notifications_priority_min()})` },
					{ value: '-1', label: `-1 (${m.notifications_priority_low()})` },
					{ value: '0', label: `0 (${m.none()})` },
					{ value: '1', label: `1 (${m.notifications_priority_low()})` },
					{ value: '2', label: '2' },
					{ value: '3', label: '3' },
					{ value: '4', label: `4 (${m.notifications_priority_normal()})` },
					{ value: '5', label: '5' },
					{ value: '6', label: '6' },
					{ value: '7', label: `7 (${m.notifications_priority_high()})` },
					{ value: '8', label: '8' },
					{ value: '9', label: '9' },
					{ value: '10', label: `10 (${m.notifications_priority_max()})` }
				]
			},
			{
				kind: 'input',
				key: 'title',
				id: 'gotify-title',
				label: m.title_optional(),
				placeholder: m.container_update_placeholder(),
				helpText: m.optional_title_override_for_notifications()
			},
			{
				kind: 'switch',
				key: 'disableTls',
				id: 'gotify-disable-tls',
				label: m.disable_tls(),
				description: m.use_http_instead_of_https_not_recommended_for_production()
			},
			{
				kind: 'switch',
				key: 'insecureSkipVerify',
				id: 'gotify-insecure-skip-verify',
				label: m.skip_tls_verification(),
				description: m.skip_tls_verification_help()
			},
			{
				kind: 'switch',
				key: 'useHeader',
				id: 'gotify-use-header',
				label: m.notifications_gotify_use_header_label(),
				description: m.notifications_gotify_use_header_help()
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
						label: m.server_host(),
						placeholder: m.notifications_matrix_host_placeholder(),
						helpText: m.notifications_matrix_host_help(),
						wrapperClass: 'md:col-span-3'
					},
					{
						kind: 'input',
						key: 'port',
						id: 'matrix-port',
						label: m.port_optional(),
						placeholder: m.notifications_matrix_port_placeholder(),
						helpText: m.server_port_leave_at_0_to_use_default_443_for_https_80_for_http(),
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
				label: m.common_username(),
				placeholder: m.username_placeholder(),
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
				label: m.disable_tls(),
				description: m.use_http_instead_of_https_not_recommended_for_production()
			}
		],
		googlechat: [
			{
				kind: 'input',
				key: 'webhookUrl',
				id: 'googlechat-webhook-url',
				label: m.webhook_url(),
				placeholder: m.notifications_googlechat_webhook_url_placeholder(),
				helpText: m.notifications_googlechat_webhook_url_help()
			}
		],
		generic: [
			{
				kind: 'input',
				key: 'webhookUrl',
				id: 'generic-webhook-url',
				label: m.webhook_url(),
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
			},
			{
				kind: 'textarea',
				key: 'payloadTemplate',
				id: 'generic-payload-template',
				label: m.notifications_generic_payload_template_label(),
				placeholder: GENERIC_PAYLOAD_TEMPLATE_PLACEHOLDER,
				helpText: `${m.notifications_generic_payload_template_help()} ${GENERIC_PAYLOAD_TEMPLATE_VARS}`,
				rows: 4
			},
			{
				kind: 'input',
				key: 'successBodyContains',
				id: 'generic-success-body-contains',
				label: m.notifications_generic_success_body_label(),
				placeholder: m.notifications_generic_success_body_placeholder(),
				helpText: m.notifications_generic_success_body_help()
			}
		]
	};

	const testOptions: NotificationProviderTestOption[] = getDefaultNotificationProviderTestOptions();

	const validation = $derived.by(() => providerSchemas[provider].safeParse(values));
	const fieldErrors = $derived.by(() =>
		mapZodFieldErrors<AnyBuiltInValues>(validation as z.ZodSafeParseResult<AnyBuiltInValues>)
	);
	const selectedSchema = $derived(providerFormSchemas[provider] as ProviderFormSchema<AnyBuiltInValues>);
	const selectedMeta = $derived(getNotificationProviderDefinition(provider));

	export function isValid(): boolean {
		return validation.success;
	}
</script>

<ProviderFormWrapper
	id={provider}
	title={selectedMeta.label()}
	description={selectedMeta.description()}
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
		bind:eventAutoHeal={values.eventAutoHeal}
		{disabled}
	/>

	<NotificationProviderTestMenu {disabled} {isTesting} {onTest} options={testOptions} />
</ProviderFormWrapper>

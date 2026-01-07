import { m } from '$lib/paraglide/messages';
import { z } from 'zod/v4';

const emailSchema = z.email();
const commaSeparatedEmailsSchema = z
	.string()
	.min(1, 'At least one email address is required')
	.refine(
		(val) => {
			const emails = val
				.split(',')
				.map((e) => e.trim())
				.filter((e) => e.length > 0);
			return emails.length > 0 && emails.every((e) => emailSchema.safeParse(e).success);
		},
		{ message: 'Invalid email address format' }
	);

export interface ProviderField {
	name: string;
	label: () => string;
	type: 'text' | 'password' | 'number' | 'switch' | 'select';
	required?: boolean;
	placeholder?: () => string;
	description?: () => string;
	options?: { value: string; label: string }[];
	defaultValue?: any;
	validate?: z.ZodSchema;
}

export const providerSchemas: Record<string, ProviderField[]> = {
	discord: [
		{
			name: 'webhookUrl',
			label: () => m.notification_field_webhook_url(),
			type: 'text',
			required: true,
			placeholder: () => 'https://discord.com/api/webhooks/...'
		}
	],
	email: [
		{
			name: 'fromAddress',
			label: () => m.notification_field_from_address(),
			type: 'text',
			required: true,
			placeholder: () => 'arcane@example.com',
			validate: emailSchema
		},
		{
			name: 'toAddresses',
			label: () => m.notification_field_to_addresses(),
			type: 'text',
			required: true,
			description: () => 'Comma separated list of email addresses',
			validate: commaSeparatedEmailsSchema
		},
		{
			name: 'smtpHost',
			label: () => m.notification_field_smtp_host(),
			type: 'text',
			required: true,
			placeholder: () => 'smtp.gmail.com'
		},
		{
			name: 'smtpPort',
			label: () => m.notification_field_smtp_port(),
			type: 'number',
			required: true,
			defaultValue: 587
		},
		{
			name: 'smtpUsername',
			label: () => m.notification_field_smtp_username(),
			type: 'text',
			required: false
		},
		{
			name: 'smtpPassword',
			label: () => m.notification_field_smtp_password(),
			type: 'password',
			required: false
		},
		{
			name: 'tlsMode',
			label: () => m.notification_field_tls_mode(),
			type: 'select',
			options: [
				{ value: 'none', label: m.notification_field_tls_mode_none() },
				{ value: 'starttls', label: m.notification_field_tls_mode_starttls() },
				{ value: 'ssl', label: m.notification_field_tls_mode_ssl() }
			],
			defaultValue: 'starttls'
		},
		{
			name: 'skipTLSVerify',
			label: () => m.notification_field_skip_tls_verify(),
			type: 'switch',
			required: false,
			defaultValue: false,
			description: () => m.notification_field_skip_tls_verify_description()
		}
	],
	gotify: [
		{
			name: 'url',
			label: () => m.notification_field_server_url(),
			type: 'text',
			required: true,
			placeholder: () => 'https://gotify.example.com'
		},
		{
			name: 'token',
			label: () => m.notification_field_token(),
			type: 'password',
			required: true
		},
		{
			name: 'priority',
			label: () => m.notification_field_priority(),
			type: 'number',
			required: false,
			defaultValue: 0
		}
	],
	ntfy: [
		{
			name: 'url',
			label: () => m.notification_field_server_url(),
			type: 'text',
			required: true,
			placeholder: () => 'https://ntfy.sh'
		},
		{
			name: 'topic',
			label: () => m.notification_field_topic(),
			type: 'text',
			required: true
		},
		{
			name: 'priority',
			label: () => m.notification_field_priority(),
			type: 'number',
			required: false,
			defaultValue: 3
		},
		{
			name: 'username',
			label: () => m.common_username(),
			type: 'text',
			required: false
		},
		{
			name: 'password',
			label: () => m.common_password(),
			type: 'password',
			required: false
		}
	],
	pushbullet: [
		{
			name: 'accessToken',
			label: () => m.notification_field_access_token(),
			type: 'password',
			required: true
		},
		{
			name: 'channelTag',
			label: () => m.notification_field_channel_tag(),
			type: 'text',
			required: false
		}
	],
	pushover: [
		{
			name: 'token',
			label: () => m.notification_field_token(),
			type: 'password',
			required: true
		},
		{
			name: 'userKey',
			label: () => m.notification_field_user_key(),
			type: 'text',
			required: true
		},
		{
			name: 'priority',
			label: () => m.notification_field_priority(),
			type: 'number',
			required: false,
			defaultValue: 0
		},
		{
			name: 'sound',
			label: () => m.notification_field_sound(),
			type: 'text',
			required: false
		}
	],
	slack: [
		{
			name: 'webhookUrl',
			label: () => m.notification_field_webhook_url(),
			type: 'text',
			required: true,
			placeholder: () => 'https://hooks.slack.com/services/...'
		}
	],
	telegram: [
		{
			name: 'botToken',
			label: () => m.notification_field_bot_token(),
			type: 'password',
			required: true
		},
		{
			name: 'chatId',
			label: () => m.notification_field_chat_id(),
			type: 'text',
			required: true
		},
		{
			name: 'sendSilently',
			label: () => m.notification_field_send_silently(),
			type: 'switch',
			required: false,
			defaultValue: false
		}
	],
	webhook: [
		{
			name: 'webhookUrl',
			label: () => m.notification_field_webhook_url(),
			type: 'text',
			required: true
		}
	],
	generic: [
		{
			name: 'url',
			label: () => m.notification_field_server_url(),
			type: 'text',
			required: true,
			description: () => 'Full Shoutrrr URL'
		}
	]
};

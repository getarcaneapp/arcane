import * as m from '$lib/paraglide/messages';

export type EmailTLSMode = 'none' | 'starttls' | 'ssl';

// Simple record of provider value => label function
export const notificationProviders = {
	bark: m.notification_provider_bark,
	discord: m.notification_provider_discord,
	email: m.notification_provider_email,
	gotify: m.notification_provider_gotify,
	googlechat: m.notification_provider_googlechat,
	ifttt: m.notification_provider_ifttt,
	join: m.notification_provider_join,
	mattermost: m.notification_provider_mattermost,
	matrix: m.notification_provider_matrix,
	ntfy: m.notification_provider_ntfy,
	opsgenie: m.notification_provider_opsgenie,
	pushbullet: m.notification_provider_pushbullet,
	pushover: m.notification_provider_pushover,
	rocketchat: m.notification_provider_rocketchat,
	slack: m.notification_provider_slack,
	teams: m.notification_provider_teams,
	telegram: m.notification_provider_telegram,
	zulip: m.notification_provider_zulip,
	webhook: m.notification_provider_webhook
};

export type NotificationProvider = keyof typeof notificationProviders;

export const NotificationProviderUrlFormats: Record<string, string> = {
	bark: 'bark://devicekey@host',
	discord: 'discord://token@id',
	email: 'smtp://username:password@host:port/?from=fromAddress&to=recipient1[,recipient2,...]',
	gotify: 'gotify://gotify-host/token',
	googlechat: 'googlechat://chat.googleapis.com/v1/spaces/FOO/messages?key=bar&token=baz',
	ifttt: 'ifttt://key/?events=event1[,event2,...]&value1=value1&value2=value2&value3=value3',
	join: 'join://shoutrrr:api-key@join/?devices=device1[,device2,...][&icon=icon][&title=title]',
	mattermost: 'mattermost://[username@]mattermost-host/token[/channel]',
	matrix: 'matrix://username:password@host:port/[?rooms=!roomID1[,roomAlias2]]',
	ntfy: 'ntfy://username:password@ntfy.sh/topic',
	opsgenie: 'opsgenie://host/token?responders=responder1[,responder2]',
	pushbullet: 'pushbullet://api-token[/device/#channel/email]',
	pushover: 'pushover://shoutrrr:apiToken@userKey/?devices=device1[,device2,...]',
	rocketchat: 'rocketchat://[username@]rocketchat-host/token[/channel|@recipient]',
	slack: 'slack://[botname@]token-a/token-b/token-c',
	teams: 'teams://group@tenant/altId/groupOwner?host=organization.webhook.office.com',
	telegram: 'telegram://token@telegram?chats=@channel-1[,chat-id-1,...]',
	zulip: 'zulip://bot-mail:bot-key@zulip-domain/?stream=name-or-id&topic=name',
	webhook: 'webhook://<url>'
};

export interface NotificationSettings {
	id: string;
	name: string;
	provider: string;
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

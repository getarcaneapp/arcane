export type WebhookTargetType = 'container' | 'project' | 'updater' | 'gitops';

export type Webhook = {
	id: string;
	name: string;
	tokenPrefix: string;
	targetType: WebhookTargetType;
	targetId: string;
	environmentId: string;
	enabled: boolean;
	lastTriggeredAt?: string;
	createdAt: string;
};

export type WebhookCreated = {
	id: string;
	name: string;
	token: string;
	targetType: WebhookTargetType;
	targetId: string;
	createdAt: string;
};

export type CreateWebhook = {
	name: string;
	targetType: WebhookTargetType;
	targetId: string;
};

export type UpdateWebhook = {
	enabled: boolean;
};

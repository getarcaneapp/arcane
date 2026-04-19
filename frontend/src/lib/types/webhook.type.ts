export type WebhookTargetType = 'container' | 'project' | 'updater' | 'gitops';
export type WebhookActionType = 'update' | 'start' | 'stop' | 'restart' | 'redeploy' | 'up' | 'down' | 'run' | 'sync';

export type Webhook = {
	id: string;
	name: string;
	tokenPrefix: string;
	targetType: WebhookTargetType;
	actionType: WebhookActionType;
	targetId: string;
	targetName?: string;
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
	actionType: WebhookActionType;
	targetId: string;
	createdAt: string;
};

export type CreateWebhook = {
	name: string;
	targetType: WebhookTargetType;
	actionType: WebhookActionType;
	targetId: string;
};

export type UpdateWebhook = {
	enabled: boolean;
};

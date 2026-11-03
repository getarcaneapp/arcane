import type { Webhook } from '../webhook';
import type { CreateRequest, UpdateRequest } from './pagination';

export type WebhookWritable = 'name' | 'targetType' | 'actionType' | 'targetId' | 'enabled';

export type WebhookCreate = CreateRequest<Webhook, WebhookWritable, 'name' | 'targetType' | 'actionType' | 'targetId'>;

export type WebhookUpdate = UpdateRequest<Webhook, 'enabled'>;

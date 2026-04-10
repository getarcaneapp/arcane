import { webhookService } from '$lib/services/webhook-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const webhooks = await webhookService.getWebhooks();
	return { webhooks };
};

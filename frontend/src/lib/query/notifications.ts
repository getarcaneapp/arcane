import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { apnsService } from '#lib/services/apns-service.js';
import { notificationService } from '#lib/services/notification-service.js';

export function notificationSettingsQuery() {
	return queryOptions({
		queryKey: queryKeys.notifications.settings(),
		queryFn: () => notificationService.getSettings()
	});
}

export function apnsStatusQuery() {
	return queryOptions({
		queryKey: queryKeys.notifications.apnsStatus(),
		queryFn: () => apnsService.getStatus()
	});
}

import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { DashboardWidgetConfig } from '#lib/types/auth.js';
import type { Environment } from '#lib/types/environment.js';
import { hasPermission } from '#lib/utils/auth.js';

import { shouldLoadEnvironment } from '../../overview.helpers';

export type WidgetTarget =
	| { status: 'unconfigured' | 'missing' | 'denied' | 'offline' }
	| { status: 'ready'; environment: Environment; resourceId: string };

export function resolveWidgetTarget(config: DashboardWidgetConfig, permissions: string[]): WidgetTarget {
	const environmentId = config.environmentIds?.[0];
	if (!environmentId || !config.resourceId) return { status: 'unconfigured' };
	const environment = environmentStore.available.find((entry) => entry.id === environmentId && entry.enabled);
	if (!environment) return { status: 'missing' };
	if (!permissions.every((permission) => hasPermission(permission, environment.id))) return { status: 'denied' };
	if (!shouldLoadEnvironment(environment)) return { status: 'offline' };
	return { status: 'ready', environment, resourceId: config.resourceId };
}

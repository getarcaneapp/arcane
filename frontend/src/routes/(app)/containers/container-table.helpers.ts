import { m } from '#lib/paraglide/messages.js';
import type { ContainerSummaryDto } from '#lib/types/docker.js';
import { COMPOSE_PROJECT_LABEL } from '#lib/utils/docker.js';

export type ActionStatus =
	| 'starting'
	| 'stopping'
	| 'restarting'
	| 'pausing'
	| 'unpausing'
	| 'killing'
	| 'updating'
	| 'removing'
	| 'redeploying'
	| '';
export type StateBadgeVariant = 'green' | 'red' | 'amber';

/** The lowest published port, else the lowest private one, as the backend sorts ports. */
function lowestPort(container: ContainerSummaryDto): number | null {
	const ports = container.ports ?? [];
	const lowest = (values: number[]) => (values.length > 0 ? Math.min(...values) : null);
	return (
		lowest(ports.flatMap((port) => (port.publicPort ? [port.publicPort] : []))) ??
		lowest(ports.flatMap((port) => (port.privatePort > 0 ? [port.privatePort] : [])))
	);
}

/** Sort value for a container column whose sort key is not a row field. */
export function containerSortValue(container: ContainerSummaryDto, column: string): unknown {
	switch (column) {
		case 'name':
			return container.names?.[0];
		case 'cpuUsage':
			return container.resourceSample?.cpuPercent ?? null;
		case 'memoryUsage':
			return container.resourceSample?.memoryUsageBytes ?? null;
		case 'ports':
			return lowestPort(container);
		// The updates table sorts its image column as imageRef.
		case 'imageRef':
			return container.image;
		default:
			return container[column as keyof ContainerSummaryDto];
	}
}

export function getContainerDisplayName(container: ContainerSummaryDto): string {
	return container.names?.[0] || container.id.substring(0, 12);
}

const actionStatusMessages: Record<ActionStatus, () => string> = {
	starting: () => m.common_action_starting(),
	stopping: () => m.common_action_stopping(),
	restarting: () => m.common_action_restarting(),
	pausing: () => m.common_action_pausing(),
	unpausing: () => m.common_action_unpausing(),
	killing: () => m.common_action_killing(),
	redeploying: () => m.common_action_redeploying(),
	updating: () => m.common_action_updating(),
	removing: () => m.common_action_removing(),
	'': () => ''
};

export function getActionStatusMessage(status: ActionStatus): string {
	return actionStatusMessages[status]();
}

export function getStateBadgeVariant(state: string): StateBadgeVariant {
	if (state === 'running') return 'green';
	if (state === 'exited') return 'red';
	return 'amber';
}

export function getProjectName(container: ContainerSummaryDto): string {
	const projectLabel = container.labels?.[COMPOSE_PROJECT_LABEL];
	return projectLabel || 'No Project';
}

export function getContainerStatusLabel(state: string): string {
	switch (state) {
		case 'created':
			return m.common_created();
		case 'restarting':
			return m.containers_status_restarting();
		case 'running':
			return m.common_running();
		case 'removing':
			return m.containers_status_removing();
		case 'paused':
			return m.paused();
		case 'exited':
			return m.containers_status_exited();
		case 'dead':
			return m.containers_status_dead();
		default:
			return state ? state.charAt(0).toUpperCase() + state.slice(1) : m.common_unknown();
	}
}

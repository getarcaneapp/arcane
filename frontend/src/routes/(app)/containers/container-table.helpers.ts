import { m } from '$lib/paraglide/messages';
import type { ContainerSummaryDto } from '$lib/types/container.type';

export type ActionStatus = 'starting' | 'stopping' | 'restarting' | 'updating' | 'removing' | '';
export type StateBadgeVariant = 'green' | 'red' | 'amber';

export function parseImageRef(imageRef: string): { repo: string; tag: string } {
	// Handle images like "nginx:latest", "library/nginx:1.0", "ghcr.io/org/image:tag"
	const lastColon = imageRef.lastIndexOf(':');
	// Check if colon is part of a tag (not a port in registry URL)
	const hasTag = lastColon > 0 && !imageRef.substring(lastColon).includes('/');

	if (hasTag) {
		return {
			repo: imageRef.substring(0, lastColon),
			tag: imageRef.substring(lastColon + 1)
		};
	}
	return { repo: imageRef, tag: 'latest' };
}

export function getContainerDisplayName(container: ContainerSummaryDto): string {
	if (container.names && container.names.length > 0) {
		return container.names[0].replace(/^\//, '');
	}
	return container.id.substring(0, 12);
}

const actionStatusMessages: Record<ActionStatus, () => string> = {
	starting: () => m.common_action_starting(),
	stopping: () => m.common_action_stopping(),
	restarting: () => m.common_action_restarting(),
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

export function getContainerIpAddress(container: ContainerSummaryDto): string | null {
	const networks = container.networkSettings?.networks;
	if (!networks) return null;
	for (const networkName in networks) {
		const network = networks[networkName];
		if (network?.ipAddress) return network.ipAddress;
	}
	return null;
}

export function getProjectName(container: ContainerSummaryDto): string {
	const projectLabel = container.labels?.['com.docker.compose.project'];
	return projectLabel || 'No Project';
}

export function groupContainerByProject(container: ContainerSummaryDto): string {
	return getProjectName(container);
}

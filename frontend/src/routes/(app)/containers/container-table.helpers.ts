import { m } from '$lib/paraglide/messages';
import type { ContainerSummaryDto } from '$lib/types/docker';

export type ActionStatus = 'starting' | 'stopping' | 'restarting' | 'updating' | 'removing' | 'redeploying' | '';
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
	const first = container.names?.[0];
	if (first) {
		return first.replace(/^\//, '');
	}
	return container.id.substring(0, 12);
}

const actionStatusMessages: Record<ActionStatus, () => string> = {
	starting: () => m.common_action_starting(),
	stopping: () => m.common_action_stopping(),
	restarting: () => m.common_action_restarting(),
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

export function getContainerIpAddresses(container: ContainerSummaryDto): string[] {
	const networks = container.networkSettings?.networks;
	if (!networks) return [];

	const seen = new Set<string>();
	const ipAddresses: string[] = [];
	for (const networkName of Object.keys(networks).sort((a, b) => a.localeCompare(b))) {
		const ipAddress = networks[networkName]?.ipAddress?.trim();
		if (!ipAddress || seen.has(ipAddress)) continue;

		seen.add(ipAddress);
		ipAddresses.push(ipAddress);
	}

	return ipAddresses;
}

export function getProjectName(container: ContainerSummaryDto): string {
	const projectLabel = container.labels?.['com.docker.compose.project'];
	return projectLabel || 'No Project';
}

export function groupContainerByProject(container: ContainerSummaryDto): string {
	return getProjectName(container);
}

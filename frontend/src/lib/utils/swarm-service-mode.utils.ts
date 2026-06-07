import { m } from '$lib/paraglide/messages';
import type { SwarmServiceModeName, SwarmServiceModeSpec } from '$lib/types/swarm.type';

export type SwarmServiceModeBadgeVariant = 'green' | 'blue' | 'amber' | 'purple' | 'gray';

export function getSwarmServiceModeFromSpec(mode: SwarmServiceModeSpec | undefined): SwarmServiceModeName {
	if (mode?.Replicated) return 'replicated';
	if (mode?.Global !== undefined) return 'global';
	if (mode?.ReplicatedJob) return 'replicated-job';
	if (mode?.GlobalJob !== undefined) return 'global-job';
	return 'unknown';
}

export function getSwarmServiceModeLabel(mode: string): string {
	switch (mode) {
		case 'replicated':
			return m.swarm_service_mode_replicated();
		case 'global':
			return m.swarm_service_mode_global();
		case 'replicated-job':
			return m.swarm_service_mode_replicated_job();
		case 'global-job':
			return m.swarm_service_mode_global_job();
		default:
			return m.common_unknown();
	}
}

export function getSwarmServiceModeVariant(mode: string): SwarmServiceModeBadgeVariant {
	switch (mode) {
		case 'replicated':
			return 'blue';
		case 'global':
			return 'green';
		case 'replicated-job':
			return 'amber';
		case 'global-job':
			return 'purple';
		default:
			return 'gray';
	}
}

export function isSwarmServiceModeScalable(mode: string): boolean {
	return mode === 'replicated' || mode === 'replicated-job';
}

import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import { PODMAN_SYSTEMD_UNIT_LABEL, type PodmanStatus } from '#lib/types/podman.js';
import { environmentHref, readableEnvironments } from '#lib/utils/environments.js';

export type PodmanStateVariant = 'green' | 'red' | 'amber' | 'gray';

export function podmanStateVariant(state: string | undefined): PodmanStateVariant {
	const normalized = (state ?? '').toLowerCase();
	if (normalized === 'running' || normalized === 'active') return 'green';
	if (['exited', 'stopped', 'dead', 'failed', 'error', 'inactive'].includes(normalized)) return 'red';
	if (!normalized || normalized === 'unknown' || normalized.startsWith('not loaded')) return 'gray';
	return 'amber';
}

export function quadletUnitHref(labels: Record<string, string> | null | undefined, environmentId: string): string | undefined {
	const unit = labels?.[PODMAN_SYSTEMD_UNIT_LABEL];
	return unit && !unit.startsWith('podman-compose@')
		? environmentHref(`/podman/quadlets?unit=${encodeURIComponent(unit)}`, environmentId)
		: undefined;
}

export function shownPodmanEnvironments(
	statuses: Record<string, PodmanStatus>,
	supports: (status: PodmanStatus) => boolean = () => true
): EnvironmentRef[] {
	return readableEnvironments('podman:read').filter((env) => {
		const status = statuses[env.id];
		return status !== undefined && supports(status);
	});
}

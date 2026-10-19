import { PODMAN_SYSTEMD_UNIT_LABEL } from '#lib/types/podman.js';

export type PodmanStateVariant = 'green' | 'red' | 'amber' | 'gray';

export function podmanStateVariant(state: string | undefined): PodmanStateVariant {
	const normalized = (state ?? '').toLowerCase();
	if (normalized === 'running' || normalized === 'active') return 'green';
	if (['exited', 'stopped', 'dead', 'failed', 'error', 'inactive'].includes(normalized)) return 'red';
	if (!normalized || normalized === 'unknown' || normalized.startsWith('not loaded')) return 'gray';
	return 'amber';
}

export function quadletUnitHref(labels: Record<string, string> | null | undefined): string | undefined {
	const unit = labels?.[PODMAN_SYSTEMD_UNIT_LABEL];
	return unit ? `/podman/quadlets?unit=${encodeURIComponent(unit)}` : undefined;
}

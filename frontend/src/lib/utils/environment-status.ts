import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';

type RuntimeEnvironmentState = Pick<Environment, 'isEdge' | 'connected' | 'status' | 'lastPollAt'>;

export function resolveEnvironmentStatus(
	environment: RuntimeEnvironmentState,
	overrideStatus?: EnvironmentStatus | null
): EnvironmentStatus {
	const status = overrideStatus ?? environment.status;

	if (!environment.isEdge) {
		return status;
	}

	if (environment.connected === true) {
		return 'online';
	}

	if (environment.lastPollAt) {
		return 'standby';
	}

	if (status === 'pending') {
		return 'pending';
	}

	if (status === 'standby') {
		return 'standby';
	}

	if (environment.connected === false) {
		return 'offline';
	}

	return status;
}

export function isEnvironmentOnline(environment: RuntimeEnvironmentState, overrideStatus?: EnvironmentStatus | null): boolean {
	const resolved = resolveEnvironmentStatus(environment, overrideStatus);
	return resolved === 'online' || resolved === 'standby';
}

export function getEnvironmentStatusVariant(status: EnvironmentStatus): 'green' | 'blue' | 'amber' | 'red' {
	switch (status) {
		case 'online':
			return 'green';
		case 'standby':
			return 'blue';
		case 'pending':
			return 'amber';
		default:
			return 'red';
	}
}

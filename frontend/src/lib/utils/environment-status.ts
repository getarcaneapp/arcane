import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';

type RuntimeEnvironmentState = Pick<Environment, 'isEdge' | 'connected' | 'status'>;

export function resolveEnvironmentStatus(
	environment: RuntimeEnvironmentState,
	overrideStatus?: EnvironmentStatus | null
): EnvironmentStatus {
	const status = overrideStatus ?? environment.status;

	if (!environment.isEdge) {
		return status;
	}

	if (status === 'pending') {
		return 'pending';
	}

	if (environment.connected === true) {
		return 'online';
	}

	if (environment.connected === false) {
		return 'offline';
	}

	return status;
}

export function isEnvironmentOnline(environment: RuntimeEnvironmentState, overrideStatus?: EnvironmentStatus | null): boolean {
	return resolveEnvironmentStatus(environment, overrideStatus) === 'online';
}

export function getEnvironmentStatusVariant(status: EnvironmentStatus): 'green' | 'amber' | 'red' {
	switch (status) {
		case 'online':
			return 'green';
		case 'pending':
			return 'amber';
		default:
			return 'red';
	}
}

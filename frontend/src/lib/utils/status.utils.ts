export type StatusVariant = 'red' | 'purple' | 'green' | 'blue' | 'gray' | 'amber';

const STATUS_VARIANT_MAP: Record<string, StatusVariant> = {
	running: 'green',
	deployed: 'green',
	stopped: 'red',
	failed: 'red',
	pending: 'amber',
	creating: 'blue',
	updating: 'blue',
	deleting: 'purple',
	exited: 'red'
};

export function getStatusVariant(status?: string | null): StatusVariant {
	if (!status) return 'gray';
	return STATUS_VARIANT_MAP[String(status).toLowerCase()] ?? 'gray';
}

export { STATUS_VARIANT_MAP as statusVariantMap };

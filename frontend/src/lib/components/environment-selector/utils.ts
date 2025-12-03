import type { Environment } from './types';

export function getStatusColor(status: string): string {
	return status === 'online' ? 'bg-emerald-500' : status === 'offline' ? 'bg-red-500' : 'bg-yellow-500';
}

export function getConnectionString(env: Environment): string {
	return env.apiUrl.replace(/^https?:\/\//, '').replace(/\/$/, '');
}


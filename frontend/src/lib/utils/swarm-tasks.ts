import type { SwarmTaskSummary } from '$lib/types/swarm';

const SWARM_TASK_STATE_ORDER: Record<string, number> = {
	running: 0,
	starting: 1,
	pending: 2,
	ready: 3,
	complete: 4,
	shutdown: 5,
	failed: 6,
	rejected: 7,
	orphaned: 8,
	remove: 9
};

export function getSwarmTaskStateVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
	if (state === 'running') return 'green';
	if (state === 'pending' || state === 'starting') return 'amber';
	if (state === 'failed' || state === 'rejected' || state === 'shutdown') return 'red';
	return 'gray';
}

export function getSwarmTaskIconVariant(state: string): 'emerald' | 'amber' | 'red' | 'gray' {
	if (state === 'running') return 'emerald';
	if (state === 'pending' || state === 'starting') return 'amber';
	if (state === 'failed' || state === 'rejected' || state === 'shutdown') return 'red';
	return 'gray';
}

export function sortSwarmTasks(raw: SwarmTaskSummary[]): SwarmTaskSummary[] {
	return [...raw].sort((a, b) => {
		const stateA = SWARM_TASK_STATE_ORDER[a.currentState] ?? 99;
		const stateB = SWARM_TASK_STATE_ORDER[b.currentState] ?? 99;
		if (stateA !== stateB) return stateA - stateB;
		return new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime();
	});
}

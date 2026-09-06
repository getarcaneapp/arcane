import type { SwarmTaskSummary } from '#lib/types/swarm.js';
import { instantEpochMilliseconds } from '#lib/utils/formatting.js';

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

const taskStateVariants = new Map<string, 'green' | 'amber' | 'red'>([
	['running', 'green'],
	['pending', 'amber'],
	['starting', 'amber'],
	['failed', 'red'],
	['rejected', 'red'],
	['shutdown', 'red']
]);

export function getSwarmTaskStateVariant(state: string): 'green' | 'amber' | 'red' | 'gray' {
	return taskStateVariants.get(state) ?? 'gray';
}

export function getSwarmTaskIconVariant(state: string): 'emerald' | 'amber' | 'red' | 'gray' {
	const variant = getSwarmTaskStateVariant(state);
	return variant === 'green' ? 'emerald' : variant;
}

export function sortSwarmTasks(raw: SwarmTaskSummary[]): SwarmTaskSummary[] {
	const updatedMs = new Map(raw.map((t) => [t, instantEpochMilliseconds(t.updatedAt) ?? 0]));
	return [...raw].sort((a, b) => {
		const stateA = SWARM_TASK_STATE_ORDER[a.currentState] ?? 99;
		const stateB = SWARM_TASK_STATE_ORDER[b.currentState] ?? 99;
		if (stateA !== stateB) return stateA - stateB;
		return (updatedMs.get(b) ?? 0) - (updatedMs.get(a) ?? 0);
	});
}

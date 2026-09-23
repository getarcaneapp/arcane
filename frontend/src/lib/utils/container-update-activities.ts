import { activityStore } from '#lib/stores/activity.store.svelte.js';
import type { Activity } from '#lib/types/activity.type.js';

/** Tracks accepted updates and updates that were already running when a view mounted. */
export function createContainerUpdateActivityTracker(
	environmentId: () => string,
	onTerminal: (activity: Activity) => void,
	resourceId?: () => string
) {
	const activeIds = new Set<string>();
	const acceptedIds = new Set<string>();
	const terminalIds = new Set<string>();
	const refreshedIds = new Set<string>();
	let observedKey = `${environmentId()}:${resourceId?.() ?? ''}`;

	function observe(activities: readonly Activity[]) {
		const currentKey = `${environmentId()}:${resourceId?.() ?? ''}`;
		if (observedKey !== currentKey) {
			activeIds.clear();
			acceptedIds.clear();
			terminalIds.clear();
			refreshedIds.clear();
			observedKey = currentKey;
		}
		for (const activity of activities) {
			if (
				activity.type !== 'auto_update' ||
				activity.resourceType !== 'container' ||
				(activity.sourceEnvironmentId || activity.environmentId) !== environmentId() ||
				(resourceId && activity.resourceId !== resourceId())
			)
				continue;
			if (activity.status === 'queued' || activity.status === 'running') {
				activeIds.add(activity.id);
				continue;
			}
			if (terminalIds.has(activity.id)) continue;
			terminalIds.add(activity.id);
			if (activeIds.has(activity.id) || acceptedIds.has(activity.id)) {
				refreshedIds.add(activity.id);
				onTerminal(activity);
			}
			activeIds.delete(activity.id);
			acceptedIds.delete(activity.id);
		}
	}

	function accept(activity: Activity) {
		if (refreshedIds.has(activity.id)) return;
		acceptedIds.add(activity.id);
		terminalIds.delete(activity.id);
		observe([activity, ...activityStore.activities]);
	}

	return { observe, accept };
}

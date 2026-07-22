import { activityStore } from '$lib/stores/activity.store.svelte';
import { markActivityToastShown } from '$lib/components/activity/activity-completion-toasts';
import { m } from '$lib/paraglide/messages';

export function activityToastOptions(activityId?: string) {
	if (!activityId) {
		return undefined;
	}

	// The caller is about to show a response toast for this activity; keep the
	// stream-driven completion toast from duplicating it.
	markActivityToastShown(activityId);

	return {
		action: {
			label: m.activity_view_activity(),
			onClick: () => activityStore.openCenter(activityId)
		}
	};
}

export function extractActivityId(value: unknown): string | undefined {
	if (!value || typeof value !== 'object') {
		return undefined;
	}

	const activityId = (value as { activityId?: unknown }).activityId;
	if (typeof activityId === 'string' && activityId.trim()) {
		return activityId;
	}

	if (Array.isArray(value)) {
		for (const item of value) {
			const nested = extractActivityId(item);
			if (nested) return nested;
		}
		return undefined;
	}

	for (const item of Object.values(value)) {
		const nested = extractActivityId(item);
		if (nested) return nested;
	}

	return undefined;
}

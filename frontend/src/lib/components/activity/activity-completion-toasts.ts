import { toast } from 'svelte-sonner';
import { PersistedState } from 'runed';
import { m } from '$lib/paraglide/messages';
import type { Activity, ActivityType } from '$lib/types/activity.type';
import { activityStore } from '$lib/stores/activity.store.svelte';
import { activityTypeLabel } from './activity-labels';

/**
 * In-the-moment success/failure toasts for activities the current user
 * started, shown when the Activity Center sheet is closed. Completions are
 * buffered briefly and grouped (by batch, falling back to type+status) so a
 * bulk operation produces one aggregated toast instead of one per item.
 *
 * Quick actions (container start/stop, project down, ...) are excluded: they
 * complete within their HTTP request and the action call sites already show a
 * response toast. Activities whose call site showed such a toast are also
 * suppressed here (see markActivityToastShown) so nothing fires twice.
 */

export const activityCompletionToastsEnabled = new PersistedState<boolean>('arcane-activity-toasts', true);

const DEBOUNCE_MS = 1500;
const MAX_WAIT_MS = 4000;

// How long a direct response toast suppresses the completion toast for the
// same activity. Covers the debounce window plus response/stream skew.
const SHOWN_SUPPRESSION_MS = 15_000;

const LONG_RUNNING_TYPES = new Set<ActivityType>([
	'image_pull',
	'image_build',
	'image_update_check',
	'project_pull',
	'project_build',
	'project_deploy',
	'project_redeploy',
	'container_redeploy',
	'vulnerability_scan',
	'auto_update',
	'system_prune'
]);

let pending: Activity[] = [];
let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let hardFlushTimer: ReturnType<typeof setTimeout> | null = null;
const shownActivityIds = new Map<string, number>();

// Records that the UI already showed a response toast for this activity, so
// the stream-driven completion toast stays silent for it.
export function markActivityToastShown(activityId: string | undefined) {
	if (!activityId) {
		return;
	}
	const now = Date.now();
	for (const [id, shownAt] of shownActivityIds) {
		if (now - shownAt > SHOWN_SUPPRESSION_MS) {
			shownActivityIds.delete(id);
		}
	}
	shownActivityIds.set(activityId, now);
}

function wasToastShownInternal(activityId: string): boolean {
	const shownAt = shownActivityIds.get(activityId);
	return shownAt !== undefined && Date.now() - shownAt <= SHOWN_SUPPRESSION_MS;
}

export function queueActivityCompletionToast(activity: Activity) {
	if (!activityCompletionToastsEnabled.current) {
		return;
	}
	if (!LONG_RUNNING_TYPES.has(activity.type)) {
		return;
	}
	pending.push(activity);
	if (debounceTimer) {
		clearTimeout(debounceTimer);
	}
	debounceTimer = setTimeout(flushInternal, DEBOUNCE_MS);
	// Cap how long a steady stream of completions can defer the first toast.
	hardFlushTimer ??= setTimeout(flushInternal, MAX_WAIT_MS);
}

export function discardPendingActivityToasts() {
	clearTimersInternal();
	pending = [];
}

function clearTimersInternal() {
	if (debounceTimer) {
		clearTimeout(debounceTimer);
		debounceTimer = null;
	}
	if (hardFlushTimer) {
		clearTimeout(hardFlushTimer);
		hardFlushTimer = null;
	}
}

function flushInternal() {
	clearTimersInternal();
	const items = pending;
	pending = [];
	if (items.length === 0) {
		return;
	}

	const groups = new Map<string, Activity[]>();
	for (const activity of items) {
		const key = activity.batchId ?? `${activity.type}:${activity.status}`;
		const group = groups.get(key);
		if (group) {
			group.push(activity);
		} else {
			groups.set(key, [activity]);
		}
	}
	for (const members of groups.values()) {
		// A response toast (single or bulk summary) already covered this work.
		if (members.some((activity) => wasToastShownInternal(activity.id))) {
			continue;
		}
		emitGroupToastInternal(members);
	}
}

// Container activities often carry a 64-char hex ID as their resource name;
// show the familiar 12-char short ID instead.
function shortenDockerIdInternal(value: string): string {
	return /^[0-9a-f]{25,64}$/i.test(value) ? value.slice(0, 12) : value;
}

function emitGroupToastInternal(members: Activity[]) {
	const first = members[0];
	if (!first) {
		return;
	}

	if (members.length === 1) {
		const rawName = first.resourceName || first.resourceId || '';
		const name = rawName ? shortenDockerIdInternal(rawName) : activityTypeLabel(first.type);
		const action = viewActionInternal(first.id, first.batchId);
		if (first.status === 'failed') {
			toast.error(m.activity_toast_failed({ name }), { action });
		} else {
			toast.success(m.activity_toast_success({ name }), { action });
		}
		return;
	}

	const type = activityTypeLabel(first.type);
	const failed = members.filter((activity) => activity.status === 'failed').length;
	const action = viewActionInternal(undefined, first.batchId);
	if (failed > 0) {
		toast.error(m.activity_toast_batch_partial({ type, failed, total: members.length }), { action });
	} else {
		toast.success(m.activity_toast_batch_success({ type, count: members.length }), { action });
	}
}

function viewActionInternal(activityId: string | undefined, batchId: string | undefined) {
	return {
		label: m.activity_toast_view(),
		onClick: () => activityStore.openCenter(activityId, batchId)
	};
}

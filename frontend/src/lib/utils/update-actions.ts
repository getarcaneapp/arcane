import { toast } from 'svelte-sonner';
import { openConfirmDialog } from '#lib/components/confirm-dialog';
import { m } from '#lib/paraglide/messages';
import BaseAPIService from '#lib/services/api-service';
import { imageService } from '#lib/services/image-service';
import { userService } from '#lib/services/user-service';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import { activityToastOptions } from '#lib/utils/activity-toast';
import type { AutoUpdateResourceType, AutoUpdateResult } from '#lib/types/automation';

/**
 * Helpers around the updater run endpoint (`POST /environments/{id}/updater/run`),
 * so the row actions, the bulk actions and Update All all summarize an
 * `AutoUpdateResult` the same way.
 */

/** Applies pending updates for a single resource via a scoped updater run. */
export function applyScopedUpdate(type: Extract<AutoUpdateResourceType, 'container' | 'project'>, id: string) {
	return imageService.runAutoUpdate({ type, resourceIds: [id] });
}

/**
 * The updater reports per-resource failures in the body of a 200, so a resolved
 * promise alone does not mean the update landed. Bulk runners tally by
 * rejection, so surface those failures as a throw before they get counted as
 * successes.
 */
export function throwOnUpdateFailure<T extends Pick<AutoUpdateResult, 'failed' | 'items'>>(result: T): T {
	if ((result?.failed ?? 0) > 0) {
		const firstError = result.items?.find((item) => item.status === 'failed')?.error;
		throw new Error(firstError || m.updates_bulk_update_failed());
	}
	return result;
}

/** Emits a single toast describing an updater run's updated/failed/skipped tally. */
export function summarizeUpdateResult(result: AutoUpdateResult) {
	const options = activityToastOptions(result.activityId);
	const updated = (result.updated ?? 0) + (result.restarted ?? 0);
	const failed = result.failed ?? 0;

	if (failed > 0 && updated > 0) {
		toast.warning(m.updates_apply_partial({ updated, failed }), options);
	} else if (failed > 0) {
		toast.error(m.updates_apply_failed({ failed }), options);
	} else if (updated > 0) {
		toast.success(m.updates_apply_success({ updated }), options);
	} else {
		toast.info(m.image_update_up_to_date_title(), options);
	}
}

type ConfirmAndApplyAllUpdatesOptions = {
	setLoading?: (loading: boolean) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

const MANAGER_ENVIRONMENT_ID = '0';
const SELF_UPGRADE_PROBE_INTERVAL_MS = 2000;
/** Backstop so a backend that never returns can't leave the flag armed all session. */
const SELF_UPGRADE_MAX_WAIT_MS = 5 * 60 * 1000;

/**
 * Disarms the upgrade flag as soon as this backend answers an *authenticated*
 * request again.
 *
 * A queued Arcane self-update fires last, so the restart's version-mismatch
 * 401s land after the response — and the engine reports a self-update as an
 * ordinary `updated` item, so the payload can't say whether one happened.
 * Probing settles it: when nothing restarted, the first probe succeeds and the
 * recovery window closes in about one request instead of masking an unrelated
 * expired session for minutes. While the backend is down the probe throws, so
 * the flag stays armed for exactly as long as it is needed.
 *
 * The probe must be authenticated. A public endpoint (`/app-version`) answers
 * 200 the instant the new backend binds its port, which would disarm before
 * any authenticated request could trigger recovery. `/auth/me` instead returns
 * the version-mismatch 401 itself, and — because it is not in the API client's
 * skip-auth paths — that 401 drives the refresh-and-reload while still armed.
 */
async function disarmWhenBackendResponds() {
	const deadline = Date.now() + SELF_UPGRADE_MAX_WAIT_MS;
	for (;;) {
		try {
			await userService.getCurrentUser();
			break;
		} catch {
			if (Date.now() >= deadline) break;
			await new Promise((resolve) => setTimeout(resolve, SELF_UPGRADE_PROBE_INTERVAL_MS));
		}
	}
	BaseAPIService.setUpgradeInProgress(false);
}

/**
 * Confirms, then applies every pending update on the host (an empty-body
 * updater run — the same path the nightly auto-update job takes).
 */
export function confirmAndApplyAllUpdates({ setLoading, onRefresh }: ConfirmAndApplyAllUpdatesOptions) {
	openConfirmDialog({
		title: m.update_all(),
		message: m.updates_update_all_confirm_message(),
		confirm: {
			label: m.update_all(),
			destructive: false,
			action: async () => {
				setLoading?.(true);
				// An unscoped run can pick up Arcane's own container and route it
				// through self-upgrade, restarting this backend. Only the local
				// manager hosts this frontend's API, so only it needs the flag that
				// makes the restart a recoverable reconnect instead of a logout.
				const envId = await environmentStore.getCurrentEnvironmentId();
				const restartsThisBackend = envId === MANAGER_ENVIRONMENT_ID;
				if (restartsThisBackend) BaseAPIService.setUpgradeInProgress(true);
				try {
					summarizeUpdateResult(await imageService.runAutoUpdate());
					await onRefresh?.();
				} catch (error) {
					console.error('Update all failed:', error);
					toast.error(m.updates_apply_all_failed());
				} finally {
					if (restartsThisBackend) void disarmWhenBackendResponds();
					setLoading?.(false);
				}
			}
		}
	});
}

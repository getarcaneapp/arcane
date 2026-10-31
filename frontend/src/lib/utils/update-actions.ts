import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import BaseAPIService from '#lib/services/api-service.js';
import { imageService } from '#lib/services/image-service.js';
import { userService } from '#lib/services/user-service.js';
import type { AutoUpdateResourceType, AutoUpdateResult } from '#lib/types/automation.js';
import { activityToastOptions } from '#lib/utils/activity-toast.js';
import { acrossEnvironmentsNote, actionEnvironments, settleAcrossEnvironments } from '#lib/utils/environments.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function applyScopedUpdate(
	type: Extract<AutoUpdateResourceType, 'container' | 'project'>,
	id: string,
	environmentId: string
) {
	return imageService.runAutoUpdate({ type, resourceIds: [id] }, environmentId);
}

type ThrowOnUpdateFailureOptions = {
	rejectSkipped?: boolean;
};

export function throwOnUpdateFailure<T extends Pick<AutoUpdateResult, 'failed' | 'items'>>(
	result: T,
	{ rejectSkipped = false }: ThrowOnUpdateFailureOptions = {}
): T {
	if ((result?.failed ?? 0) > 0) {
		const firstError = result.items?.find((item) => item.status === 'failed')?.error;
		throw new Error(firstError || m.updates_bulk_update_failed());
	}
	if (rejectSkipped) {
		const skipped = result?.items?.find((item) => item.status === 'skipped');
		if (skipped) {
			throw new Error(skipped.error || m.containers_update_skipped_no_reason());
		}
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
	onRefresh?: () => unknown;
};

const MANAGER_ENVIRONMENT_ID = '0';
const SELF_UPGRADE_PROBE_INTERVAL_MS = 2000;
const SELF_UPGRADE_MAX_WAIT_MS = 5 * 60 * 1000;

async function disarmWhenBackendResponds() {
	const deadline = Date.now() + SELF_UPGRADE_MAX_WAIT_MS;
	for (;;) {
		const result = await tryCatch(userService.getCurrentUser());
		if (result.error === null || Date.now() >= deadline) break;
		await new Promise((resolve) => setTimeout(resolve, SELF_UPGRADE_PROBE_INTERVAL_MS));
	}
	BaseAPIService.setUpgradeInProgress(false);
}

export function confirmAndApplyAllUpdates({ setLoading, onRefresh }: ConfirmAndApplyAllUpdatesOptions) {
	const environments = actionEnvironments('image-updates:check');
	openConfirmDialog({
		title: m.update_all(),
		message: `${m.updates_update_all_confirm_message()} ${acrossEnvironmentsNote(environments)}`.trim(),
		confirm: {
			label: m.update_all(),
			destructive: false,
			action: async () => {
				setLoading?.(true);
				// An unscoped run can pick up Arcane's own container and route it
				// through self-upgrade, restarting this backend. Only the local
				// manager hosts this frontend's API, so only it needs the flag that
				// makes the restart a recoverable reconnect instead of a logout.
				const restartsThisBackend = environments.some((env) => env.id === MANAGER_ENVIRONMENT_ID);
				if (restartsThisBackend) BaseAPIService.setUpgradeInProgress(true);
				try {
					const operationResult = await tryCatch(
						(async () => {
							const results = await settleAcrossEnvironments(environments, (id) => imageService.runAutoUpdate(undefined, id));
							for (const { value } of results) summarizeUpdateResult(value);
							await onRefresh?.();
						})()
					);
					if (operationResult.error !== null) {
						const error = operationResult.error;
						console.error('Update all failed:', error);
						toast.error(m.updates_apply_all_failed());
					}
				} finally {
					if (restartsThisBackend) void disarmWhenBackendResponds();
					setLoading?.(false);
				}
			}
		}
	});
}

export function summarizeUpdateCheckResult(result: { errorMessage?: string }) {
	const firstError = result.errorMessage?.trim();
	if (firstError) toast.error(firstError || m.containers_check_updates_failed());
	else toast.success(m.images_update_check_completed());
}

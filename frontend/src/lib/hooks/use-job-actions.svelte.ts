import { createMutation } from '@tanstack/svelte-query';
import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { jobScheduleService } from '#lib/services/job-schedule-service.js';
import type { JobStatus } from '#lib/types/settings.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';

type JobActionOptions = {
	job: JobStatus;
	environmentId: string;
	isAgent: boolean;
	durableRuns: boolean;
	enabledOverride?: boolean;
	onScheduleUpdate?: () => void;
};

export function useJobActions(getOptions: () => JobActionOptions) {
	const runMutation = createMutation(() => ({
		mutationFn: (jobId: string = getOptions().job.id) => jobScheduleService.runJob(jobId, getOptions().environmentId),
		retry: false,
		onSuccess: () => {
			toast.success(m.jobs_run_queued());
			getOptions().onScheduleUpdate?.();
		},
		onError: (err) => toast.error(m.jobs_run_now(), { description: extractApiErrorMessage(err) })
	}));
	const restartMutation = createMutation(() => ({
		mutationFn: () => jobScheduleService.restartWorker(getOptions().job.id, getOptions().environmentId),
		onSuccess: () => {
			toast.success(m.jobs_worker_restarted());
			getOptions().onScheduleUpdate?.();
		},
		onError: (err) => toast.error(m.jobs_restart_worker(), { description: extractApiErrorMessage(err) })
	}));

	const options = $derived(getOptions());
	const run = $derived(options.job.currentRun ?? options.job.lastRun);
	const isEnabled = $derived(options.enabledOverride ?? options.job.enabled);
	const requiresSave = $derived(isEnabled && !options.job.enabled);
	const managerLocked = $derived(options.isAgent && options.job.managerOnly);
	const canRun = $derived(
		options.durableRuns && isEnabled && !requiresSave && options.job.canRunManually && !runMutation.isPending && !managerLocked
	);
	const canEditSchedule = $derived(isEnabled && !!options.job.settingsKey && !managerLocked);
	const canRestartWorker = $derived(options.durableRuns && options.job.isContinuous && isEnabled);
	const description = $derived.by(() => {
		if (requiresSave) return m.jobs_save_before_run();
		if (options.job.id.startsWith('environment-health:')) return m.jobs_health_scope_description();
		return options.job.description;
	});

	return {
		runMutation,
		restartMutation,
		get run() {
			return run;
		},
		get isEnabled() {
			return isEnabled;
		},
		get managerLocked() {
			return managerLocked;
		},
		get canRun() {
			return canRun;
		},
		get canEditSchedule() {
			return canEditSchedule;
		},
		get canRestartWorker() {
			return canRestartWorker;
		},
		get description() {
			return description;
		}
	};
}

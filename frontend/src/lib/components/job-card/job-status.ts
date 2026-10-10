import { m } from '#lib/paraglide/messages.js';
import type { JobStatus } from '#lib/types/settings.js';
export function jobStatusLabel(status: string, isWorker = false): string {
	const labels: Record<string, () => string> = {
		queued: m.jobs_status_queued,
		waiting: m.jobs_status_waiting,
		running: m.jobs_status_running,
		retrying: m.jobs_status_retrying,
		succeeded: m.jobs_status_succeeded,
		partial: m.jobs_status_partial,
		skipped: m.jobs_status_skipped,
		failed: m.jobs_status_failed,
		needs_attention: isWorker ? m.jobs_status_needs_attention : m.jobs_status_failed,
		canceled: m.jobs_status_canceled,
		healthy: m.jobs_status_healthy,
		degraded: m.jobs_status_degraded,
		stopped: m.jobs_status_stopped,
		starting: m.jobs_status_starting
	};
	return labels[status]?.() ?? m.jobs_status_unknown();
}

/** Label for a workflow step, falling back to its name. */
export function jobStepLabel(name: string): string {
	const labels: Record<string, () => string> = {
		prepare: m.jobs_step_prepare,
		discover: m.jobs_step_discover,
		check: m.jobs_step_check,
		scan: m.jobs_step_scan,
		patch: m.jobs_step_patch,
		sync: m.jobs_step_sync,
		backup: m.jobs_step_backup,
		tags: m.jobs_step_tags,
		finalize: m.jobs_step_finalize,
		resume: m.jobs_step_resume,
		candidates: m.jobs_step_candidates,
		plan: m.jobs_step_plan,
		apply: m.jobs_step_apply
	};
	return labels[name]?.() ?? name;
}

export function jobNameLabel(job: JobStatus): string {
	const [group = '', ...target] = job.id.split(':');
	const labels: Record<string, () => string> = {
		'gitops-sync': m.gitops,
		'volume-backup': m.jobs_volume_backup_group,
		'system-backup': m.jobs_system_backup_group
	};
	if (group === 'environment-health' && target.length) return m.jobs_health_check();
	const label = labels[group]?.();
	if (!label) return job.name;
	return target.length ? m.jobs_dynamic_job_name({ name: label, target: target.join(':') }) : label;
}

export type JobPanelSection = 'overview' | 'history';

export type JobStatusTone = 'green' | 'red' | 'amber' | 'blue' | 'gray';

/** Badge tone for a run or worker status. */
export function jobStatusTone(status: string | undefined): JobStatusTone {
	switch (status) {
		case 'succeeded':
		case 'healthy':
			return 'green';
		case 'failed':
		case 'needs_attention':
		case 'stopped':
			return 'red';
		case 'partial':
		case 'degraded':
		case 'retrying':
		case 'waiting':
			return 'amber';
		case 'running':
		case 'queued':
		case 'starting':
			return 'blue';
		default:
			return 'gray';
	}
}

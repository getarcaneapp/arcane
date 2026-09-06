import type { JobStatus } from '#lib/types/settings';
import { m } from '#lib/paraglide/messages';
export function jobStatusLabel(status: string): string {
	const labels: Record<string, () => string> = {
		queued: m.jobs_status_queued,
		waiting: m.jobs_status_waiting,
		running: m.jobs_status_running,
		retrying: m.jobs_status_retrying,
		succeeded: m.jobs_status_succeeded,
		partial: m.jobs_status_partial,
		skipped: m.jobs_status_skipped,
		failed: m.jobs_status_failed,
		needs_attention: m.jobs_status_needs_attention,
		canceled: m.jobs_status_canceled,
		healthy: m.jobs_status_healthy,
		degraded: m.jobs_status_degraded,
		stopped: m.jobs_status_stopped,
		starting: m.jobs_status_starting
	};
	return labels[status]?.() ?? m.jobs_status_unknown();
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

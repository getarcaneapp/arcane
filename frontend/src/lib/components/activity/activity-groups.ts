import { m } from '#lib/paraglide/messages.js';
import type { Activity, ActivityGroup, ActivityStatus } from '#lib/types/activity.type.js';
import { formatDate, plainDateFromInstant } from '#lib/utils/formatting.js';

import { activityTypeLabel } from './activity-labels';

export type ActivitySection = {
	id: string;
	label: string;
	groups: ActivityGroup[];
};

export function activityGroupKey(group: ActivityGroup): string {
	return group.kind === 'batch' ? `batch:${group.batchId}` : group.activity.id;
}

// The job run summarizes a scheduled batch; otherwise the newest member leads.
export function activityGroupLead(group: ActivityGroup): Activity {
	if (group.kind === 'single') return group.activity;
	return group.items.find((item) => item.type === 'job_run') ?? group.items[0]!;
}

export function activityGroupMembers(group: ActivityGroup): Activity[] {
	if (group.kind === 'single') return [group.activity];
	return group.items.filter((item) => item.type !== 'job_run');
}

export function activityGroupStatus(group: ActivityGroup): ActivityStatus {
	return group.kind === 'batch' ? group.status : group.activity.status;
}

export function activityGroupTitle(group: ActivityGroup): string {
	return activityTypeLabel(activityGroupLead(group).type);
}

export function activityGroupTarget(group: ActivityGroup): string {
	const lead = activityGroupLead(group);
	if (group.kind === 'batch') {
		return lead.type === 'job_run' && lead.resourceName ? lead.resourceName : m.activity_batch_items({ count: group.total });
	}
	return lead.resourceName || lead.resourceId || '';
}

export function activityGroupEnvironment(group: ActivityGroup): string {
	const lead = activityGroupLead(group);
	return lead.sourceEnvironmentName || lead.sourceEnvironmentId || lead.environmentId;
}

export function isActiveActivityStatus(status: ActivityStatus): boolean {
	return status === 'running' || status === 'queued';
}

export function findActivityGroup(groups: ActivityGroup[], key: string): ActivityGroup | undefined {
	return groups.find((group) => activityGroupKey(group) === key);
}

function dayLabelInternal(value: string): { id: string; label: string } {
	const date = plainDateFromInstant(value);
	const today = plainDateFromInstant(Temporal.Now.instant());
	if (!date || !today) return { id: 'unknown', label: m.common_na() };
	const daysAgo = date.until(today, { largestUnit: 'day' }).days;
	if (daysAgo === 0) return { id: date.toString(), label: m.activity_day_today() };
	if (daysAgo === 1) return { id: date.toString(), label: m.activity_day_yesterday() };
	return { id: date.toString(), label: formatDate(value, { dateStyle: 'medium' }) };
}

// Running work first, then finished work bucketed by the day it ended.
export function buildActivitySections(running: ActivityGroup[], history: ActivityGroup[]): ActivitySection[] {
	const sections: ActivitySection[] = [];
	if (running.length > 0) {
		sections.push({ id: 'running', label: m.activity_section_running(), groups: running });
	}
	const days = new Map<string, ActivitySection>();
	for (const group of history) {
		const lead = activityGroupLead(group);
		const day = dayLabelInternal(lead.endedAt || lead.startedAt);
		const existing = days.get(day.id);
		if (existing) {
			existing.groups.push(group);
			continue;
		}
		const section = { ...day, groups: [group] };
		days.set(day.id, section);
		sections.push(section);
	}
	return sections;
}

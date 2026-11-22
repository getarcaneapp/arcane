import type { Attachment } from 'svelte/attachments';

import { m } from '#lib/paraglide/messages.js';
import type { UpgradeJobResult, UpgradeJobResultStatus, UpgradeJobStage } from '#lib/types/system.js';

const completedColors = {
	badge: 'border-success/40 bg-success/10 text-success',
	text: 'text-muted-foreground'
};

const warningColors = {
	badge: 'border-warning/40 bg-warning/10 text-warning',
	text: 'text-warning'
};

export const upgradeStatusDisplay: Record<UpgradeJobResultStatus, { label: () => string; badge: string; text: string }> = {
	pending: {
		label: m.common_pending,
		badge: 'border-border bg-muted/40 text-muted-foreground/60',
		text: 'text-muted-foreground'
	},
	updating: {
		label: m.common_action_updating,
		badge: 'border-primary/40 bg-primary/10 text-primary',
		text: 'text-primary'
	},
	updated: { label: m.common_updated, ...completedColors },
	up_to_date: { label: m.image_update_up_to_date_title, ...completedColors },
	triggered: { label: m.environments_update_all_status_triggered, ...warningColors },
	skipped_offline: { label: m.environments_update_all_status_skipped_offline, ...warningColors },
	failed: {
		label: m.common_failed,
		badge: 'border-destructive/40 bg-destructive/10 text-destructive',
		text: 'text-destructive'
	}
};

export const upgradeStages: Array<{ stage: UpgradeJobStage; label: () => string }> = [
	{ stage: 'checking', label: m.environments_update_all_stage_checking },
	{ stage: 'starting', label: m.environments_update_all_stage_starting },
	{ stage: 'pulling', label: m.environments_update_all_stage_pulling },
	{ stage: 'upgrading', label: m.environments_update_all_stage_upgrading },
	{ stage: 'reconnecting', label: m.environments_update_all_stage_reconnecting },
	{ stage: 'verifying', label: m.environments_update_all_stage_verifying }
];

export const upgradeStageLabels: Record<UpgradeJobStage, () => string> = {
	checking: m.environments_update_all_stage_checking,
	starting: m.environments_update_all_stage_starting,
	pulling: m.environments_update_all_stage_pulling,
	upgrading: m.environments_update_all_stage_upgrading,
	reconnecting: m.environments_update_all_stage_reconnecting,
	verifying: m.environments_update_all_stage_verifying
};

export function formatUpgradeVersions(result: Pick<UpgradeJobResult, 'fromVersion' | 'toVersion'>): string {
	const from = result.fromVersion?.trim() ?? '';
	const to = result.toVersion?.trim() ?? '';
	if (!from || !to || from === to) return to || from;
	return `${from} → ${to}`;
}

export function createLogTailAttachment(getLength: () => number): Attachment<HTMLPreElement> {
	return (node) => {
		void getLength();
		node.scrollTop = node.scrollHeight;
	};
}

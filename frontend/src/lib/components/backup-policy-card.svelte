<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button';
	import { Badge } from '#lib/components/ui/badge';
	import { EditIcon } from '#lib/icons';
	import type { BackupPolicy, BackupStatus } from '#lib/types/backup';
	import { backupDestinationFromFlags, backupDestinationLabel, backupStatusLabel } from '#lib/utils/backups';
	import { formatDateTimeShort } from '#lib/utils/formatting';
	import * as m from '#lib/paraglide/messages.js';

	type CardPolicy = BackupPolicy & {
		s3DestinationName?: string;
		s3Bucket?: string;
		lastRun?: { status: BackupStatus; createdAt: string };
	};

	let {
		policy,
		showStopContainers = false,
		onEdit,
		editDisabled = false
	}: {
		policy: CardPolicy;
		showStopContainers?: boolean;
		onEdit?: () => void;
		editDisabled?: boolean;
	} = $props();

	const s3Name = $derived(policy.s3DestinationName || policy.s3Bucket || policy.s3DestinationId || '');
</script>

<div class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-1.5 gap-y-0.5 rounded-md border px-2 py-1">
	<Badge variant={policy.enabled ? 'green' : 'gray'}>{policy.enabled ? m.common_enabled() : m.common_disabled()}</Badge>
	<code class="truncate">{policy.schedule}</code>
	<div class="flex items-center gap-1.5 justify-self-end">
		<Badge variant="blue">{backupDestinationLabel(backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled))}</Badge>
		{#if onEdit}
			<ArcaneButton
				action="edit"
				size="icon"
				icon={EditIcon}
				showLabel={false}
				customLabel={m.jobs_edit_schedule()}
				onclick={onEdit}
				class="size-7"
				disabled={editDisabled}
			/>
		{/if}
	</div>
	<div class="col-span-2 flex min-w-0 items-center gap-1.5">
		<span>
			{policy.retentionCount === 0
				? m.volume_backup_retention_all()
				: m.volume_backup_retention_summary({ count: policy.retentionCount })}
		</span>
		{#if showStopContainers}
			<span>·</span>
			<span>{policy.stopContainers ? m.volume_backup_containers_stopped() : m.volume_backup_containers_running()}</span>
		{/if}
		{#if policy.lastRun}
			<span>·</span>
			<span class="truncate">{backupStatusLabel(policy.lastRun.status)} · {formatDateTimeShort(policy.lastRun.createdAt)}</span>
		{/if}
	</div>
	{#if policy.s3Enabled && s3Name}
		<span class="max-w-28 justify-self-end truncate" title={s3Name}>{s3Name}</span>
	{/if}
</div>

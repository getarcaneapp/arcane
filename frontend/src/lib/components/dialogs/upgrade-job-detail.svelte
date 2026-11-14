<script lang="ts">
	import type { Attachment } from 'svelte/attachments';

	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { MANAGER_ENVIRONMENT_ID } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJobResult, UpgradeJobResultStatus, UpgradeJobStage } from '#lib/types/system.js';
	import { cn } from '#lib/utils.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';

	let { result }: { result: UpgradeJobResult } = $props();

	const statusDisplay: Record<UpgradeJobResultStatus, { label: () => string; text: string }> = {
		pending: { label: m.common_pending, text: 'text-muted-foreground' },
		updating: { label: m.common_action_updating, text: 'text-primary' },
		updated: { label: m.common_updated, text: 'text-muted-foreground' },
		up_to_date: { label: m.image_update_up_to_date_title, text: 'text-muted-foreground' },
		triggered: { label: m.environments_update_all_status_triggered, text: 'text-warning' },
		skipped_offline: { label: m.environments_update_all_status_skipped_offline, text: 'text-warning' },
		failed: { label: m.common_failed, text: 'text-destructive' }
	};

	const stages: Array<{ stage: UpgradeJobStage; label: () => string }> = [
		{ stage: 'checking', label: m.environments_update_all_stage_checking },
		{ stage: 'starting', label: m.environments_update_all_stage_starting },
		{ stage: 'pulling', label: m.environments_update_all_stage_pulling },
		{ stage: 'upgrading', label: m.environments_update_all_stage_upgrading },
		{ stage: 'reconnecting', label: m.environments_update_all_stage_reconnecting },
		{ stage: 'verifying', label: m.environments_update_all_stage_verifying }
	];

	const display = $derived(statusDisplay[result.status] ?? statusDisplay.pending);
	const isManager = $derived(result.environmentId === MANAGER_ENVIRONMENT_ID);
	const logs = $derived(result.logs ?? []);
	const stageIndex = $derived(stages.findIndex((entry) => entry.stage === result.stage));

	const versions = $derived.by(() => {
		const from = result.fromVersion?.trim() ?? '';
		const to = result.toVersion?.trim() ?? '';
		if (!from || !to || from === to) return to || from;
		return `${from} → ${to}`;
	});

	const followLogTail: Attachment<HTMLPreElement> = (node) => {
		void logs.length;
		node.scrollTop = node.scrollHeight;
	};
</script>

<div class="flex min-w-0 flex-col gap-3 px-5 py-4 text-sm">
	<div class="min-w-0">
		<div class="flex items-center gap-1.5">
			<h3 class="truncate font-semibold">{result.environmentName}</h3>
			{#if isManager}
				<span class="shrink-0 rounded-xl border px-1 text-3xs text-muted-foreground">{m.manager()}</span>
			{/if}
		</div>
		{#if result.status === 'failed' && result.error}
			<p class="text-xs wrap-break-word text-destructive">{result.error}</p>
		{:else}
			<p class="text-xs text-muted-foreground tabular-nums">
				<span class={display.text}>{display.label()}</span>
				{#if versions}<span>· {versions}</span>{/if}
			</p>
		{/if}
	</div>

	{#if result.status === 'updating'}
		<div class="flex items-center gap-2">
			<Progress value={result.percent} class="h-1.5" />
			<span class="w-9 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{result.percent}%</span>
		</div>

		<ol class="space-y-1.5">
			{#each stages as entry, index (entry.stage)}
				<li
					class={cn(
						'flex items-center gap-2',
						index < stageIndex && 'text-muted-foreground',
						index === stageIndex && 'font-medium',
						index > stageIndex && 'text-muted-foreground/60'
					)}
				>
					{#if index < stageIndex}
						<SuccessIcon class="size-3.5 shrink-0 text-success" />
					{:else if index === stageIndex}
						<Spinner class="size-3.5 shrink-0" />
					{:else}
						<ClockIcon class="size-3.5 shrink-0" />
					{/if}
					<span class="min-w-0 flex-1 truncate">
						{#if entry.stage === 'reconnecting' && isManager}
							{m.environments_update_all_stage_manager_restart()}
						{:else}
							{entry.label()}
						{/if}
					</span>
					{#if index === stageIndex && result.stageStartedAt}
						<UpdateAllElapsed class="shrink-0 text-xs font-normal text-muted-foreground" since={result.stageStartedAt} />
					{/if}
				</li>
			{/each}
		</ol>
	{/if}

	{#if logs.length > 0}
		<CodeBlock.Root size="sm" class="w-auto">
			<CodeBlock.Content {@attach followLogTail} wrap code={logs.join('\n')} class="max-h-40" />
		</CodeBlock.Root>
	{:else}
		<p class="text-xs text-muted-foreground italic">{m.environments_update_all_no_logs()}</p>
	{/if}
</div>

<script lang="ts">
	import type { Attachment } from 'svelte/attachments';

	import { Progress } from '#lib/components/ui/progress/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon, AlertIcon, AlertTriangleIcon, ArrowDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { MANAGER_ENVIRONMENT_ID } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJobResult, UpgradeJobResultStatus, UpgradeJobStage } from '#lib/types/system-upgrade.js';
	import { cn } from '#lib/utils.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';

	let { result, logsOpen = false }: { result: UpgradeJobResult; logsOpen?: boolean } = $props();

	let showLogs = $derived(logsOpen);

	const completedColors = {
		badge: 'border-success/40 bg-success/10 text-success',
		text: 'text-muted-foreground'
	};
	const warningColors = {
		badge: 'border-warning/40 bg-warning/10 text-warning',
		text: 'text-warning'
	};
	const statusDisplay: Record<UpgradeJobResultStatus, { label: () => string; badge: string; text: string }> = {
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

	const stageLabels: Record<UpgradeJobStage, () => string> = {
		checking: m.environments_update_all_stage_checking,
		starting: m.environments_update_all_stage_starting,
		pulling: m.environments_update_all_stage_pulling,
		upgrading: m.environments_update_all_stage_upgrading,
		reconnecting: m.environments_update_all_stage_reconnecting,
		verifying: m.environments_update_all_stage_verifying
	};

	const display = $derived(statusDisplay[result.status] ?? statusDisplay.pending);
	const isManager = $derived(result.environmentId === MANAGER_ENVIRONMENT_ID);
	const logs = $derived(result.logs ?? []);

	const label = $derived.by(() => {
		if (result.status !== 'updating' || !result.stage) return display.label();
		if (result.stage === 'reconnecting' && isManager) return m.environments_update_all_stage_manager_restart();
		return (stageLabels[result.stage] ?? display.label)();
	});

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

<li class="px-6 py-2.5 text-sm">
	<div class="flex items-center gap-3">
		<span class={cn('flex size-7 shrink-0 items-center justify-center rounded-full border', display.badge)}>
			{#if result.status === 'updated' || result.status === 'up_to_date'}
				<SuccessIcon class="size-3.5" />
			{:else if result.status === 'updating'}
				<Spinner class="size-3.5" />
			{:else if result.status === 'skipped_offline' || result.status === 'triggered'}
				<AlertIcon class="size-3.5" />
			{:else if result.status === 'failed'}
				<AlertTriangleIcon class="size-3.5" />
			{:else}
				<ClockIcon class="size-3.5" />
			{/if}
		</span>

		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-1.5">
				<span class="truncate font-medium">{result.environmentName}</span>
				{#if isManager}
					<span class="shrink-0 rounded border px-1 text-3xs text-muted-foreground">{m.manager()}</span>
				{/if}
			</div>
			{#if result.status === 'failed' && result.error}
				<span class="block truncate text-xs text-muted-foreground" title={result.error}>{result.error}</span>
			{:else if versions}
				<span class="block truncate text-xs text-muted-foreground tabular-nums">{versions}</span>
			{/if}
		</div>

		<div class={cn('max-w-48 shrink-0 text-right text-xs', display.text)}>
			<span>{label}</span>
			{#if result.status === 'updating' && result.stageStartedAt}
				<UpdateAllElapsed class="block text-muted-foreground" since={result.stageStartedAt} />
			{/if}
		</div>

		<button
			type="button"
			class="flex shrink-0 items-center gap-0.5 rounded px-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
			aria-expanded={showLogs}
			onclick={() => (showLogs = !showLogs)}
		>
			{m.common_logs()}
			<ArrowDownIcon class={cn('size-3.5 transition-transform', showLogs && 'rotate-180')} />
		</button>
	</div>

	{#if result.status === 'updating'}
		<div class="mt-2 flex items-center gap-2 pl-10">
			<Progress value={result.percent} class="h-1.5" />
			<span class="w-9 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{result.percent}%</span>
		</div>
	{/if}

	{#if showLogs}
		{#if logs.length > 0}
			<pre
				{@attach followLogTail}
				class="mt-2 ml-10 max-h-48 overflow-auto rounded-md border bg-muted/40 p-2 font-mono text-2xs leading-relaxed whitespace-pre-wrap text-muted-foreground">{logs.join(
					'\n'
				)}</pre>
		{:else}
			<p class="mt-2 ml-10 text-xs text-muted-foreground italic">{m.environments_update_all_no_logs()}</p>
		{/if}
	{/if}
</li>

<script lang="ts">
	import { slide } from 'svelte/transition';

	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon, AlertIcon, ArrowDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { MANAGER_ENVIRONMENT_ID } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJobResult } from '#lib/types/system.js';
	import { cn } from '#lib/utils.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';
	import { createLogTailAttachment, formatUpgradeVersions, upgradeStageLabels, upgradeStatusDisplay } from './upgrade-job';

	let { result, logsOpen = false }: { result: UpgradeJobResult; logsOpen?: boolean } = $props();

	let showLogs = $derived(logsOpen);

	const display = $derived(upgradeStatusDisplay[result.status] ?? upgradeStatusDisplay.pending);
	const isManager = $derived(result.environmentId === MANAGER_ENVIRONMENT_ID);
	const logs = $derived(result.logs ?? []);

	const label = $derived.by(() => {
		if (result.status !== 'updating' || !result.stage) return display.label();
		if (result.stage === 'reconnecting' && isManager) return m.environments_update_all_stage_manager_restart();
		return (upgradeStageLabels[result.stage] ?? display.label)();
	});

	const versions = $derived(formatUpgradeVersions(result));
	const followLogTail = createLogTailAttachment(() => logs.length);
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
				<AlertIcon class="size-3.5" />
			{:else}
				<ClockIcon class="size-3.5" />
			{/if}
		</span>

		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-1.5">
				<span class="truncate font-medium">{result.environmentName}</span>
				{#if isManager}
					<span class="shrink-0 rounded-xl border px-1 text-3xs text-muted-foreground">{m.manager()}</span>
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
			class="flex shrink-0 items-center gap-0.5 rounded-xl px-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
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
		<div class="mt-2 ml-10" transition:slide={{ duration: 200 }}>
			{#if logs.length > 0}
				<CodeBlock.Root size="sm" class="w-auto">
					<CodeBlock.Content {@attach followLogTail} wrap code={logs.join('\n')} class="max-h-48" />
				</CodeBlock.Root>
			{:else}
				<p class="text-xs text-muted-foreground italic">{m.environments_update_all_no_logs()}</p>
			{/if}
		</div>
	{/if}
</li>

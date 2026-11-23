<script lang="ts">
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { MANAGER_ENVIRONMENT_ID } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJobResult } from '#lib/types/system.js';
	import { cn } from '#lib/utils.js';

	import UpdateAllElapsed from './update-all-elapsed.svelte';
	import { createLogTailAttachment, formatUpgradeVersions, upgradeStages, upgradeStatusDisplay } from './upgrade-job';

	let { result }: { result: UpgradeJobResult } = $props();

	const display = $derived(upgradeStatusDisplay[result.status] ?? upgradeStatusDisplay.pending);
	const isManager = $derived(result.environmentId === MANAGER_ENVIRONMENT_ID);
	const logs = $derived(result.logs ?? []);
	const stageIndex = $derived(upgradeStages.findIndex((entry) => entry.stage === result.stage));
	const versions = $derived(formatUpgradeVersions(result));
	const followLogTail = createLogTailAttachment(() => logs.length);
</script>

<div class="flex min-w-0 flex-col gap-3 px-5 py-4 text-sm sm:min-h-0 sm:flex-1">
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
			{#each upgradeStages as entry, index (entry.stage)}
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
		<CodeBlock.Root size="sm" class="w-auto sm:min-h-24 sm:flex-1">
			<CodeBlock.Content {@attach followLogTail} wrap code={logs.join('\n')} class="max-h-40 sm:max-h-none sm:flex-1" />
		</CodeBlock.Root>
	{:else}
		<p class="text-xs text-muted-foreground italic">{m.environments_update_all_no_logs()}</p>
	{/if}
</div>

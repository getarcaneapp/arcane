<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { CloseIcon, TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ansiToHtml, sanitizeLogText } from '#lib/utils/formatting.js';

	import type { BuildRun } from '../build-run.svelte';

	let { run }: { run: BuildRun } = $props();

	const phaseBadge = $derived.by(() => {
		switch (run.phase) {
			case 'building':
				return { variant: 'blue' as const, label: m.progress_building() };
			case 'completed':
				return { variant: 'green' as const, label: m.completed() };
			case 'failed':
				return { variant: 'red' as const, label: m.common_failed() };
			default:
				return { variant: 'gray' as const, label: m.idle() };
		}
	});
	const plainLog = $derived(run.logLines.map((line) => sanitizeLogText(line)).join('\n'));

	function autoScrollToBottom(node: HTMLElement) {
		if (run.logLines.length > 0 && run.autoScroll) node.scrollTop = node.scrollHeight;
	}
</script>

<section class="flex h-full min-h-0 flex-col overflow-hidden rounded-xl bg-table-head">
	<div class="flex h-10 shrink-0 items-center gap-2 pr-1.5 pl-3">
		<TerminalIcon class="size-3.5 shrink-0 text-muted-foreground" />
		<h2 class="shrink-0 text-xs font-medium">{m.build_output()}</h2>
		<Badge variant={phaseBadge.variant} size="sm">{phaseBadge.label}</Badge>
		{#if run.statusText && run.phase === 'building'}
			<span class="hidden min-w-0 truncate text-xs text-muted-foreground sm:block" title={run.statusText}>
				{run.statusText}
			</span>
		{/if}
		<div class="ml-auto flex shrink-0 items-center gap-1">
			<div class="mr-1 flex items-center gap-2">
				<Switch id="build-output-auto-scroll" bind:checked={run.autoScroll} />
				<label for="build-output-auto-scroll" class="text-xs text-muted-foreground">{m.auto_scroll()}</label>
			</div>
			{#if run.logLines.length > 0}
				<CopyButton text={plainLog} class="size-6" />
				{#if !run.isBuilding}
					<ArcaneButton
						action="base"
						tone="ghost"
						size="icon"
						class="size-6"
						icon={CloseIcon}
						showLabel={false}
						customLabel={m.clear_output()}
						title={m.clear_output()}
						onclick={run.reset}
					/>
				{/if}
			{/if}
		</div>
	</div>

	{#if run.isBuilding}
		<Progress value={100} max={100} class="h-0.5 w-full" indeterminate />
	{/if}
	<div
		{@attach autoScrollToBottom}
		class="min-h-0 flex-1 overflow-auto px-3 pt-1 pb-3 font-mono text-xs-plus leading-relaxed text-foreground"
	>
		{#if run.logLines.length === 0}
			<div class="flex h-full items-center justify-center font-sans text-sm text-muted-foreground">
				{m.build_output_placeholder()}
			</div>
		{:else}
			{#each run.logLines as line, idx (idx)}
				<div class="break-words whitespace-pre-wrap">
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
					{@html ansiToHtml(line)}
				</div>
			{/each}
		{/if}
	</div>
	{#if run.error}
		<div class="shrink-0 border-t border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive">
			{run.error}
		</div>
	{/if}
</section>

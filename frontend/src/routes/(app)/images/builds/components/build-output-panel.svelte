<script lang="ts">
	import { Progress } from '#lib/components/ui/progress/index.js';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { TerminalIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ansiToHtml } from '#lib/utils/formatting.js';

	let {
		logLines,
		aggregateStatus,
		hasReachedComplete,
		buildError,
		autoScroll = $bindable(true),
		isBuilding = false,
		onReset
	}: {
		logLines: string[];
		aggregateStatus: string;
		hasReachedComplete: boolean;
		buildError: string;
		autoScroll?: boolean;
		isBuilding?: boolean;
		onReset?: () => void;
	} = $props();

	function autoScrollToBottom(node: HTMLElement) {
		logLines.length;
		if (autoScroll) node.scrollTop = node.scrollHeight;
	}
</script>

<div class="flex h-full flex-col p-8">
	<!-- Status section -->
	<div class="mb-6 shrink-0 space-y-4 rounded-2xl border border-border/50 bg-card p-5">
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-3">
				<div
					class={`size-2 rounded-full ${
						buildError
							? 'bg-destructive'
							: hasReachedComplete
								? 'bg-success'
								: isBuilding
									? 'animate-pulse bg-info'
									: 'bg-muted-foreground/30'
					}`}
				></div>
				<span class="truncate text-sm font-medium">
					{#if hasReachedComplete}
						{m.build_completed()}
					{:else}
						{aggregateStatus || m.idle()}
					{/if}
				</span>
			</div>
			<SwitchWithLabel id="auto-scroll" checked={autoScroll} label={m.auto_scroll()} onCheckedChange={(v) => (autoScroll = v)} />
		</div>
		{#if isBuilding && !hasReachedComplete}
			<Progress value={100} max={100} class="h-1.5 w-full" indeterminate />
		{/if}
	</div>

	<!-- Terminal output with refined styling -->
	<div
		{@attach autoScrollToBottom}
		class="group dark relative min-h-0 flex-1 overflow-auto rounded-2xl border border-border/50 bg-background p-5 font-mono text-xs-plus leading-relaxed text-foreground shadow-2xl shadow-black/50"
	>
		<div class="relative">
			{#if logLines.length === 0}
				<div class="flex min-h-50 items-center justify-center">
					<div class="text-center">
						<TerminalIcon class="mx-auto mb-3 size-8 text-muted-foreground/50" />
						<p class="text-sm text-muted-foreground">{m.build_output_placeholder()}</p>
					</div>
				</div>
			{:else}
				{#each logLines as line, idx (idx)}
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
					<div class="rounded px-1 py-0.5 break-words whitespace-pre-wrap transition-colors hover:bg-white/[0.03]">
						{@html ansiToHtml(line)}
					</div>
				{/each}
			{/if}
		</div>
	</div>

	<!-- Error display -->
	{#if buildError}
		<div class="mt-4 shrink-0 rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
			{buildError}
		</div>
	{/if}

	<!-- Clear button -->
	{#if logLines.length > 0 && !isBuilding}
		<div class="mt-4 shrink-0">
			<ArcaneButton action="base" tone="outline" size="sm" onclick={() => onReset?.()}>
				{m.clear_output()}
			</ArcaneButton>
		</div>
	{/if}
</div>

<script lang="ts">
	import { Progress } from '$lib/components/ui/progress/index.js';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { TerminalIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	type LayerStats = {
		total: number;
		completed: number;
		downloading: number;
		extracting: number;
	};

	type AutoScrollParams = {
		enabled: boolean;
		key: number;
	};

	let {
		logLines,
		layerStats,
		aggregateStatus,
		progressValue,
		isIndeterminate,
		hasReachedComplete,
		buildError,
		autoScroll = $bindable(true),
		isBuilding = false,
		onReset
	}: {
		logLines: string[];
		layerStats: LayerStats;
		aggregateStatus: string;
		progressValue: number;
		isIndeterminate: boolean;
		hasReachedComplete: boolean;
		buildError: string;
		autoScroll?: boolean;
		isBuilding?: boolean;
		onReset?: () => void;
	} = $props();

	function autoScrollToBottom(node: HTMLElement, params: AutoScrollParams) {
		let current = params;
		const scroll = () => {
			if (!current.enabled) return;
			node.scrollTop = node.scrollHeight;
		};
		scroll();
		return {
			update(next: AutoScrollParams) {
				current = next;
				scroll();
			}
		};
	}
</script>

<div class="flex h-full flex-col p-8">
	<!-- Progress section with enhanced styling -->
	<div
		class="mb-6 shrink-0 space-y-4 rounded-2xl border border-zinc-800/50 bg-gradient-to-br from-zinc-900/50 to-zinc-900/30 p-5"
	>
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-3">
				<div
					class={`size-2 rounded-full ${
						buildError
							? 'bg-destructive'
							: hasReachedComplete
								? 'bg-green-500'
								: isBuilding
									? 'animate-pulse bg-blue-500'
									: 'bg-muted-foreground/30'
					}`}
				></div>
				<span class="text-sm font-medium">
					{#if hasReachedComplete}
						{m.build_completed()}
					{:else if layerStats.total > 0}
						{aggregateStatus} &middot; {m.progress_layers_status({ completed: layerStats.completed, total: layerStats.total })}
					{:else}
						{aggregateStatus || m.idle()}
					{/if}
				</span>
			</div>
			<div class="flex items-center gap-3">
				{#if !isIndeterminate || hasReachedComplete}
					<span class="text-muted-foreground text-sm font-medium">{progressValue}%</span>
				{/if}
				<SwitchWithLabel
					id="auto-scroll"
					checked={autoScroll}
					label={m.auto_scroll()}
					onCheckedChange={(v) => (autoScroll = v)}
				/>
			</div>
		</div>
		<Progress
			value={hasReachedComplete || isIndeterminate ? 100 : progressValue}
			max={100}
			class="h-1.5 w-full"
			indeterminate={isIndeterminate && !hasReachedComplete}
		/>
	</div>

	<!-- Terminal output with refined styling -->
	<div
		use:autoScrollToBottom={{ enabled: autoScroll, key: logLines.length }}
		class="group relative min-h-0 flex-1 overflow-auto rounded-2xl border border-zinc-900/50 bg-gradient-to-br from-zinc-950 via-zinc-950 to-black p-5 font-mono text-[13px] leading-[1.7] text-zinc-50 shadow-2xl shadow-black/50"
	>
		<!-- Subtle scanline effect overlay -->
		<div
			class="pointer-events-none absolute inset-0 bg-gradient-to-b from-transparent via-white/[0.02] to-transparent opacity-40"
		></div>

		<div class="relative">
			{#if logLines.length === 0}
				<div class="flex min-h-[200px] items-center justify-center">
					<div class="text-center">
						<TerminalIcon class="mx-auto mb-3 size-8 text-zinc-700" />
						<p class="text-sm text-zinc-500">{m.build_output_placeholder()}</p>
					</div>
				</div>
			{:else}
				{#each logLines as line, idx (idx)}
					<div class="rounded px-1 py-0.5 break-words whitespace-pre-wrap transition-colors hover:bg-white/[0.03]">{line}</div>
				{/each}
			{/if}
		</div>
	</div>

	<!-- Error display -->
	{#if buildError}
		<div class="border-destructive/30 bg-destructive/10 text-destructive mt-4 shrink-0 rounded-xl border p-4 text-sm">
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

<script lang="ts">
	import Spinner from '#lib/components/ui/spinner/spinner.svelte';
	import { SuccessIcon, ClockIcon, AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { MANAGER_ENVIRONMENT_ID } from '#lib/stores/upgrade-job.store.svelte.js';
	import type { UpgradeJobResult } from '#lib/types/system.js';
	import { cn } from '#lib/utils.js';

	let { result, selected, onselect }: { result: UpgradeJobResult; selected: boolean; onselect: () => void } = $props();
</script>

<li>
	<button
		type="button"
		aria-current={selected}
		class={cn(
			'flex w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted/40',
			selected && 'bg-muted/60 font-medium',
			result.status === 'pending' && 'text-muted-foreground'
		)}
		onclick={onselect}
	>
		{#if result.status === 'updated' || result.status === 'up_to_date'}
			<SuccessIcon class="size-3.5 shrink-0 text-success" />
		{:else if result.status === 'updating'}
			<Spinner class="size-3.5 shrink-0" />
		{:else if result.status === 'failed'}
			<AlertIcon class="size-3.5 shrink-0 text-destructive" />
		{:else if result.status === 'skipped_offline' || result.status === 'triggered'}
			<AlertIcon class="size-3.5 shrink-0 text-warning" />
		{:else}
			<ClockIcon class="size-3.5 shrink-0 text-muted-foreground/60" />
		{/if}
		<span class="min-w-0 flex-1 truncate">{result.environmentName}</span>
		{#if result.environmentId === MANAGER_ENVIRONMENT_ID}
			<span class="shrink-0 rounded-xl border px-1 text-3xs font-normal text-muted-foreground">{m.manager()}</span>
		{/if}
		{#if result.status === 'updating'}
			<span class="shrink-0 text-xs font-normal text-primary tabular-nums">{result.percent}%</span>
		{/if}
	</button>
</li>

<script lang="ts">
	import { cn } from '#lib/utils';
	import type { IconType } from '#lib/icons';

	interface Props {
		label: string;
		value: string | number;
		unit?: string;
		sub?: string;
		icon: IconType;
		accent?: string;
	}

	let { label, value, unit, sub, icon: Icon, accent = 'text-primary' }: Props = $props();
</script>

<div class="flex items-center gap-3 rounded-lg border border-border/60 bg-card/40 px-3.5 py-3">
	<div class={cn('flex size-9 shrink-0 items-center justify-center rounded-md bg-foreground/5', accent)}>
		<Icon class="size-4" />
	</div>
	<div class="min-w-0">
		<div class="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{label}</div>
		<div class="flex items-baseline gap-1">
			<span class="text-lg leading-tight font-semibold tabular-nums">{value}</span>
			{#if unit}<span class="text-xs text-muted-foreground">{unit}</span>{/if}
		</div>
		{#if sub}<div class="truncate text-[11px] text-muted-foreground">{sub}</div>{/if}
	</div>
</div>

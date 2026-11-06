<script lang="ts">
	import type { Snippet } from 'svelte';

	import { cn } from '#lib/utils.js';

	interface Props {
		label: string;
		children: Snippet;
		/** `custom` preserves the value snippet's own formatting. */
		valueFormat?: 'code' | 'text' | 'custom';
		stacked?: boolean;
		valueTitle?: string;
		class?: string;
		compact?: boolean;
	}

	let { label, children, valueFormat = 'code', stacked = false, valueTitle, class: className, compact = false }: Props = $props();
</script>

<div class={cn('flex min-w-0 flex-col gap-1 border-b border-border/50', compact ? 'py-2' : 'py-3', className)}>
	<div class={cn('break-all text-muted-foreground', compact ? 'text-2xs' : 'text-xs')}>
		{label}
	</div>
	<div
		class={cn(
			valueFormat === 'text' && 'text-sm font-medium text-foreground',
			valueFormat === 'code' && compact && 'font-mono text-xs break-all whitespace-pre-wrap',
			valueFormat === 'code' && !compact && 'cursor-pointer font-mono text-sm break-all text-foreground select-all',
			stacked && 'flex flex-col gap-2'
		)}
		title={valueTitle}
	>
		{@render children()}
	</div>
</div>

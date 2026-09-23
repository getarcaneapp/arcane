<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils.js';

	interface Props {
		label: string;
		children: Snippet;
		/** `code` renders a selectable monospace value; `text` renders plain text. */
		valueFormat?: 'code' | 'text';
		/** Stacks multiple value children vertically. */
		stacked?: boolean;
		valueTitle?: string;
		class?: string;
		compact?: boolean;
		variant?: 'default' | 'subtle' | 'outlined';
	}

	let {
		label,
		children,
		valueFormat = 'code',
		stacked = false,
		valueTitle,
		class: className,
		compact = false,
		variant = 'subtle'
	}: Props = $props();
</script>

{#snippet content()}
	<Card.Content>
		<div class="flex flex-col gap-2">
			<div
				class={compact
					? 'text-3xs font-semibold tracking-widest text-muted-foreground uppercase'
					: 'text-xs font-semibold tracking-wide break-all text-muted-foreground uppercase'}
			>
				{label}
			</div>
			<div
				class={cn(
					valueFormat === 'text' && 'text-sm font-medium text-foreground',
					valueFormat === 'code' && compact && 'text-xs break-all whitespace-pre-wrap',
					valueFormat === 'code' &&
						!compact &&
						'cursor-pointer font-mono text-sm font-medium break-all text-foreground select-all',
					stacked && 'flex flex-col gap-2'
				)}
				title={valueTitle}
			>
				{@render children()}
			</div>
		</div>
	</Card.Content>
{/snippet}

<Card.Root {variant} size={compact ? 'sm' : 'default'} class={className}>
	{@render content()}
</Card.Root>

<script lang="ts">
	import TagIcon from '@lucide/svelte/icons/tag';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import type { Suggestion, InputMatch } from './types';

	interface Props {
		suggestions: Suggestion[];
		inputMatch: InputMatch | null;
		selectedIndex: number;
		onSelect?: (index: number) => void;
	}

	let { suggestions, inputMatch, selectedIndex = $bindable(), onSelect }: Props = $props();
</script>

{#if suggestions.length > 0}
	<div class="bg-popover border-border absolute top-full right-0 left-0 z-50 mt-1 rounded-md border shadow-md">
		<div class="text-muted-foreground px-3 py-1.5 text-xs">{m.env_selector_suggestions()}</div>
		<div class="max-h-[180px] overflow-y-auto p-1 pt-0">
			{#each suggestions as suggestion, index}
				<button
					class={cn(
						'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors',
						index === selectedIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
					)}
					onclick={() => onSelect?.(index)}
					onmouseenter={() => (selectedIndex = index)}
				>
					{#if inputMatch?.type === 'status'}
						<span class={cn('size-2 rounded-full', suggestion.value === 'online' ? 'bg-emerald-500' : 'bg-red-500')}></span>
					{:else}
						<TagIcon class="size-3.5" />
					{/if}
					<span>{suggestion.label}</span>
				</button>
			{/each}
		</div>
	</div>
{/if}

<script lang="ts">
	import { cn } from '$lib/utils';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import type { Icon as IconType } from '@lucide/svelte';
	import { onMount, type Snippet } from 'svelte';
	import { slide } from 'svelte/transition';
	import { Button } from './ui/button';
	import * as Card from './ui/card';

	let {
		id,
		title,
		description,
		defaultExpanded = false,
		icon,
		badge,
		children
	}: {
		id: string;
		title: string;
		description?: string;
		defaultExpanded?: boolean;
		icon?: typeof IconType;
		badge?: Snippet;
		children: Snippet;
	} = $props();

	let expanded = $state(false);
	let initialized = $state(false);

	function loadExpandedState() {
		const state = JSON.parse(localStorage.getItem('collapsible-cards-expanded') || '{}');
		expanded = state[id] || false;
	}

	function saveExpandedState() {
		const state = JSON.parse(localStorage.getItem('collapsible-cards-expanded') || '{}');
		state[id] = expanded;
		localStorage.setItem('collapsible-cards-expanded', JSON.stringify(state));
	}

	function toggleExpanded() {
		expanded = !expanded;
		saveExpandedState();
	}

	function onHeaderClick(e: MouseEvent) {
		const target = e.target as HTMLElement;
		if (target.closest('button, a, [onclick], [role="button"]')) return;
		toggleExpanded();
	}

	function onHeaderKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggleExpanded();
		}
	}

	onMount(() => {
		if (!initialized) {
			expanded = defaultExpanded;
			if (defaultExpanded) {
				saveExpandedState();
			}
			loadExpandedState();
			initialized = true;
		}
	});
</script>

<Card.Root>
	<Card.Header
		{icon}
		enableHover
		class="cursor-pointer border-b select-none"
		role="button"
		tabindex={0}
		onclick={onHeaderClick}
		onkeydown={onHeaderKeydown}
	>
		<div>
			<div class="flex items-center gap-2">
				<Card.Title>{title}</Card.Title>
				{#if badge}
					{@render badge()}
				{/if}
			</div>
			{#if description}
				<Card.Description class="mt-1">{description}</Card.Description>
			{/if}
		</div>
		<Card.Action>
			<Button
				variant="ghost"
				size="icon"
				class="h-8 w-8"
				aria-label={expanded ? 'Collapse section' : 'Expand section'}
				onclick={() => toggleExpanded()}
			>
				<ChevronDownIcon class={cn('size-5 transition-transform duration-200', expanded && 'rotate-180')} />
			</Button>
		</Card.Action>
	</Card.Header>
	{#if expanded}
		<div transition:slide={{ duration: 200 }}>
			<Card.Content class="pt-4 pb-6">
				{@render children()}
			</Card.Content>
		</div>
	{/if}
</Card.Root>

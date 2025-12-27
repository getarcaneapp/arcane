<script lang="ts">
	import { cn } from '$lib/utils';
	import { onMount, type Snippet } from 'svelte';
	import { slide } from 'svelte/transition';
	import * as Card from './ui/card';
	import { ArrowDownIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';

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
		icon?: typeof ArrowDownIcon;
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
		const interactive = target.closest('button, a, [onclick], [role="button"]');
		if (interactive && interactive !== e.currentTarget) return;
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
		<Card.Action class="ml-auto">
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				icon={ArrowDownIcon}
				class={cn('[&_svg]:size-5 [&_svg]:transition-transform [&_svg]:duration-200', expanded && '[&_svg]:rotate-180')}
				onclick={() => toggleExpanded()}
				aria-label={expanded ? 'Collapse section' : 'Expand section'}
			/>
		</Card.Action>
	</Card.Header>
	{#if expanded}
		<div transition:slide={{ duration: 200 }}>
			<Card.Content class="p-0">
				{@render children()}
			</Card.Content>
		</div>
	{/if}
</Card.Root>

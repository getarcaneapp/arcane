<script lang="ts">
	import * as Card from '#lib/components/ui/card';
	import type { IconType } from '#lib/icons';
	import { cn } from '#lib/utils';
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		icon?: IconType;
		iconVariant?: 'primary' | 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'cyan' | 'orange' | 'indigo' | 'pink';
		class?: string;
		headerClass?: string;
		contentClass?: string;
		actions?: Snippet;
		children: Snippet;
	}

	let {
		title,
		description,
		icon,
		iconVariant = 'primary',
		class: className,
		headerClass,
		contentClass = 'p-4',
		actions,
		children
	}: Props = $props();
</script>

<Card.Root class={className}>
	<Card.Header {icon} {iconVariant} class={headerClass}>
		<div class="flex min-w-0 flex-1 flex-col space-y-1.5">
			<Card.Title>
				<h2>{title}</h2>
			</Card.Title>
			{#if description}
				<Card.Description>{description}</Card.Description>
			{/if}
		</div>
		{#if actions}
			<Card.Action class={cn(icon && 'top-4 right-4')}>
				{@render actions()}
			</Card.Action>
		{/if}
	</Card.Header>
	<Card.Content class={contentClass}>
		{@render children()}
	</Card.Content>
</Card.Root>

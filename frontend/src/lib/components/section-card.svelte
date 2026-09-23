<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { IconType } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import type { ComponentProps, Snippet } from 'svelte';

	interface Props {
		title: string;
		variant?: ComponentProps<typeof Card.Root>['variant'];
		description?: string;
		icon?: IconType;
		iconVariant?: 'primary' | 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'cyan' | 'orange' | 'indigo' | 'pink';
		class?: string;
		/** Stacks children as a divided list of setting rows. */
		divided?: boolean;
		actions?: Snippet;
		children: Snippet;
	}

	let {
		title,
		variant = 'default',
		description,
		icon,
		iconVariant = 'primary',
		class: className,
		divided = false,
		actions,
		children
	}: Props = $props();
</script>

<Card.Root {variant} class={className}>
	<Card.Header {icon} {iconVariant}>
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
	{#if divided}
		<div class="divide-y divide-border/40 px-6 lg:p-6 lg:pt-0 [&>*]:py-5 [&>*:first-child]:pt-0 [&>*:last-child]:pb-0">
			{@render children()}
		</div>
	{:else}
		<Card.Content>
			{@render children()}
		</Card.Content>
	{/if}
</Card.Root>

<script lang="ts">
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { IconType } from '$lib/icons';
	import type { Snippet } from 'svelte';
	import type { ClassValue } from 'svelte/elements';

	interface Props {
		/** Optional icon rendered in the muted media box. */
		icon?: IconType;
		title: string;
		description?: string;
		/** Renders a single primary action button when provided. */
		actionLabel?: string;
		onAction?: () => void;
		actionHref?: string;
		class?: ClassValue;
		/** Custom action content; overrides the actionLabel button when present. */
		children?: Snippet;
	}

	let { icon: Icon, title, description, actionLabel, onAction, actionHref, class: className, children }: Props = $props();
</script>

<Empty.Root class={className}>
	<Empty.Header>
		{#if Icon}
			<Empty.Media variant="icon">
				<Icon class="text-muted-foreground size-8" aria-hidden="true" />
			</Empty.Media>
		{/if}
		<Empty.Title>{title}</Empty.Title>
		{#if description}
			<Empty.Description>{description}</Empty.Description>
		{/if}
	</Empty.Header>
	{#if children}
		<Empty.Content>
			{@render children()}
		</Empty.Content>
	{:else if actionLabel}
		<Empty.Content>
			{#if actionHref}
				<ArcaneButton action="base" customLabel={actionLabel} href={actionHref} />
			{:else}
				<ArcaneButton action="base" customLabel={actionLabel} onclick={onAction} />
			{/if}
		</Empty.Content>
	{/if}
</Empty.Root>

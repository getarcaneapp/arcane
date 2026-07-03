<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import type { Component } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { ArrowRightIcon } from '$lib/icons';

	interface Props {
		icon: Component<any>;
		href: string;
		title: string;
		description: string;
		isLoading?: boolean;
	}

	let { icon, href, title, description, isLoading = false }: Props = $props();
</script>

<Card.Header {icon} class="shrink-0">
	<div class="flex flex-1 items-center justify-between">
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2><a class="hover:underline" {href}>{title}</a></h2>
			</Card.Title>
			<Card.Description>{description}</Card.Description>
		</div>
		<ArcaneButton action="base" tone="ghost" size="sm" {href} disabled={isLoading}>
			{m.common_view_all()}
			<ArrowRightIcon class="size-4" />
		</ArcaneButton>
	</div>
</Card.Header>

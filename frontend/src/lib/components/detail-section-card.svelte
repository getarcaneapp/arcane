<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import type { Snippet } from 'svelte';

	interface Props {
		icon: any;
		title: string;
		description?: string;
		id?: string;
		children: Snippet;
	}

	let { icon, title, description, id, children }: Props = $props();
</script>

<Card.Root {id}>
	<Card.Header {icon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{title}</h2>
			</Card.Title>
			{#if description}
				<Card.Description>{description}</Card.Description>
			{/if}
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		{@render children()}
	</Card.Content>
</Card.Root>

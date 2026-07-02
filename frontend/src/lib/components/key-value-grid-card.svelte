<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { KeyValueCard, KeyValueGrid } from '$lib/components/resource-detail';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		icon: any;
		title: string;
		description: string;
		entries: [string, string][];
	}

	let { icon, title, description, entries }: Props = $props();
</script>

<Card.Root>
	<Card.Header {icon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>{title}</Card.Title>
			<Card.Description>{description}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		<KeyValueGrid>
			{#each entries as [key, value] (key)}
				<KeyValueCard label={key} valueTitle={m.common_click_to_select()}>{value}</KeyValueCard>
			{/each}
		</KeyValueGrid>
	</Card.Content>
</Card.Root>

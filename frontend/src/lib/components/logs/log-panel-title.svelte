<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		title: string;
		live: boolean;
	}

	let { title, live }: Props = $props();
</script>

<div class="flex items-center gap-2">
	<Card.Title>
		<h2>{title}</h2>
	</Card.Title>
	{#if live}
		<div class="flex items-center gap-2">
			<div class="size-2 animate-pulse rounded-full bg-green-500"></div>
			<span class="text-xs font-semibold text-green-600 sm:text-sm">{m.common_live()}</span>
		</div>
	{/if}
</div>

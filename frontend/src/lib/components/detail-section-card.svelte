<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		icon: any;
		title: string;
		description?: string;
		id?: string;
		children: Snippet;
	}

	let { icon: Icon, title, description, id, children }: Props = $props();
</script>

<section {id} class="scroll-mt-24 p-4 sm:p-5">
	<div class="mb-3 flex items-start gap-2">
		<Icon class="text-primary mt-0.5 size-4 shrink-0" />
		<div class="min-w-0">
			<h3 class="text-sm font-semibold">{title}</h3>
			{#if description}
				<p class="text-muted-foreground text-xs">{description}</p>
			{/if}
		</div>
	</div>
	{@render children()}
</section>

<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		compose: Snippet;
		env: Snippet;
		class?: string;
		composeClass?: string;
		envClass?: string;
		onsubmit?: (event: SubmitEvent) => void;
	}

	let {
		compose,
		env,
		class: className = 'flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch',
		composeClass = 'flex min-h-0 flex-1 flex-col lg:col-span-3',
		envClass = 'flex min-h-0 flex-1 flex-col lg:col-span-2',
		onsubmit
	}: Props = $props();
</script>

{#snippet content()}
	<div class={composeClass}>
		{@render compose()}
	</div>

	<div class={envClass}>
		{@render env()}
	</div>
{/snippet}

{#if onsubmit}
	<form class={className} {onsubmit}>
		{@render content()}
	</form>
{:else}
	<div class={className}>
		{@render content()}
	</div>
{/if}

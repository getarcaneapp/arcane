<script lang="ts">
	import { m } from '$lib/paraglide/messages';

	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	const pageTitle = $derived(`${m.layout_title()} | ${m.projects_title()}`);
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>

{@render children()}

<script lang="ts">
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';

	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	const projectName = $derived(page.data.project.name);
	const pageTitle = $derived(`${m.layout_title()} | ${m.projects_title()} | ${projectName}`);
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>

{@render children()}

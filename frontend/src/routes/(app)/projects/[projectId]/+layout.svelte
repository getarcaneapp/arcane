<script lang="ts">
	import type { LayoutProps } from './$types';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';

	let { children }: LayoutProps = $props();

	const projectName = $derived(page.data['project'].name);
	const pageTitle = $derived(`${m.layout_title()} | ${m.projects_title()} | ${projectName}`);
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>

{@render children()}

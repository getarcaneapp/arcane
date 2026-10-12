<script lang="ts">
	import { page } from '$app/state';

	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<!-- The page's editors read their volume once, so another volume or environment gets a fresh page. -->
{#key `${page.data['envId']}:${page.params.volumeName}`}
	{@render children()}
{/key}

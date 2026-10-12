<script lang="ts">
	import { page } from '$app/state';

	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<!-- The editor reads its container once, so another container or environment gets a fresh editor. -->
{#key `${page.data['envId']}:${page.params.containerId}`}
	{@render children()}
{/key}

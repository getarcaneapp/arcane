<script lang="ts">
	import { page } from '$app/state';

	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<!-- The page's editors read their project once, so another project or environment gets a fresh page. -->
{#key `${page.data['envId']}:${page.params.projectId}`}
	{@render children()}
{/key}

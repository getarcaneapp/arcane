<script lang="ts">
	import { resolve } from '$app/paths';

	import ResourceErrorPage from '#lib/components/resource-error-page.svelte';
</script>

<ResourceErrorPage fallbackTitle="Failed to load project" backLabel="Back to Projects" backHref={resolve('/(app)/projects')} />

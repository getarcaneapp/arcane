<script lang="ts">
	import ResourceErrorPage from '$lib/components/resource-error-page.svelte';
	import { m } from '$lib/paraglide/messages';
</script>

<ResourceErrorPage
	fallbackTitle={m.error_generic()}
	backLabel={m.common_back_to({ resource: m.containers() })}
	backHref="/containers"
/>

<script lang="ts">
	import ResourceErrorPage from '#lib/components/resource-error-page.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<ResourceErrorPage
	fallbackTitle={m.error_generic()}
	backLabel={m.common_back_to({ resource: m.podman_quadlets_title() })}
	backHref="/podman/quadlets"
/>

<script lang="ts">
	import { resolve } from '$app/paths';

	import ResourceErrorPage from '#lib/components/resource-error-page.svelte';
	import { m } from '#lib/paraglide/messages.js';
</script>

<ResourceErrorPage
	fallbackTitle={m.error_generic()}
	backLabel={m.common_back_to({ resource: m.resource_networks_cap() })}
	backHref={resolve('/(app)/networks')}
/>

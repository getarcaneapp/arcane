<script lang="ts">
	import SwarmKvPage from '#lib/components/swarm/swarm-kv-page.svelte';
	import { swarmKvResources } from '#lib/utils/swarm-kv.js';

	let { data } = $props();
</script>

<SwarmKvPage
	resource={swarmKvResources.configs}
	initialSwarm={data.swarm}
	initialItems={data.items}
	initialRequestOptions={data.requestOptions}
/>

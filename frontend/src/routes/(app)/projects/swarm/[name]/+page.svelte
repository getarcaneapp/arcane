<script lang="ts">
	import SwarmProjectEditor from '../components/SwarmProjectEditor.svelte';

	let { data } = $props();
</script>

<div class="flex h-full min-h-0 flex-1 flex-col">
	<SwarmProjectEditor {data} />
</div>

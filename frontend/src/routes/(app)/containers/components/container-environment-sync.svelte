<script lang="ts">
	import { onMount } from 'svelte';

	let {
		onActivate
	}: {
		onActivate: () => void | Promise<unknown>;
	} = $props();

	onMount(() => {
		void onActivate();
	});
</script>

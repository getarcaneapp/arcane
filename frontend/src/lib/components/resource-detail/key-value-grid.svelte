<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();
</script>

<div class="grid grid-cols-1 gap-x-8 lg:grid-cols-2 2xl:grid-cols-3">
	{@render children()}
</div>

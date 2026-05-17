<script lang="ts">
	let { children }: LayoutProps = $props();
</script>

<main class="flex-1">
	{@render children()}
</main>

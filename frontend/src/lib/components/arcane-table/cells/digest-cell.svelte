<script lang="ts">
	let { value }: { value: unknown } = $props();
	const text = $derived(typeof value === 'string' ? value : '-');
</script>

<span class="font-mono text-xs break-all whitespace-normal" title={text !== '-' ? text : undefined}>
	{text}
</span>

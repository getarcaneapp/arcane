<script lang="ts">
	let { value }: { value: unknown } = $props();
</script>

<span class="max-w-125 truncate">{value ?? ''}</span>

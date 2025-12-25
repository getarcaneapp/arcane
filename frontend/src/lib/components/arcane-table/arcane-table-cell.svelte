<script lang="ts">
	let { value }: { value: unknown } = $props();
</script>

<span class="max-w-[500px] truncate">{value ?? ''}</span>

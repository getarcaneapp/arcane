<script lang="ts">
	import { formatDateTimeShort } from '$lib/utils/formatting';

	let { value }: { value: unknown } = $props();
</script>

{formatDateTimeShort(new Date(Number(value || 0) * 1000))}

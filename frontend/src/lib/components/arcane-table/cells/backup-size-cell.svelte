<script lang="ts">
	import { bytes } from '#lib/utils/formatting.js';

	let { size }: { size: number } = $props();
</script>

{bytes.format(size, { unitSeparator: ' ' }) ?? '-'}

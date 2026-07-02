<script lang="ts">
	import { formatImageUpdateCheckedAt } from '$lib/utils/image-updates';

	let { value }: { value: unknown } = $props();
</script>

<span class="text-sm">{formatImageUpdateCheckedAt(typeof value === 'string' ? value : '')}</span>

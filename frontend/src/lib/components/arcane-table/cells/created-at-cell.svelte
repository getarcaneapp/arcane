<script lang="ts">
	import { formatDateTimeShort } from '#lib/utils/formatting';
	import { m } from '#lib/paraglide/messages';

	let { value }: { value: unknown } = $props();
</script>

<span class="text-sm">{value ? formatDateTimeShort(String(value)) : m.common_na()}</span>

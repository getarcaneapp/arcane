<script lang="ts">
	import { format } from 'date-fns';
	import { m } from '$lib/paraglide/messages';

	let { value }: { value: unknown } = $props();
</script>

<span class="text-sm">{value ? format(new Date(String(value)), 'PP p') : m.common_na()}</span>

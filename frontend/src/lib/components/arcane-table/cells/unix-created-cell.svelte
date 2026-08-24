<script lang="ts">
	import { formatDateTimeShort } from '#lib/utils/formatting';
	import { Temporal } from 'temporal-polyfill';

	let { value }: { value: unknown } = $props();

	const seconds = $derived(Number(value || 0));
</script>

{Number.isFinite(seconds) ? formatDateTimeShort(Temporal.Instant.fromEpochMilliseconds(Math.trunc(seconds * 1000))) : ''}

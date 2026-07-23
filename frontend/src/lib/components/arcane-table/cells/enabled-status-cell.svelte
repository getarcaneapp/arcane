<script lang="ts">
	import { Badge } from '#lib/components/ui/badge';
	import { m } from '#lib/paraglide/messages';

	let { value }: { value: unknown } = $props();
	const enabled = $derived(Boolean(value));
</script>

<Badge variant={enabled ? 'green' : 'red'} minWidth="20">{enabled ? m.common_enabled() : m.common_disabled()}</Badge>

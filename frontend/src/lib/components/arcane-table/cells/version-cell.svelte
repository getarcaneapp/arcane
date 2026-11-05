<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import DigestCell from './digest-cell.svelte';

	let { current, latest, wrap = false }: { current: string; latest: string; wrap?: boolean } = $props();
</script>

<dl class={cn('grid grid-cols-label-auto items-center gap-x-3 gap-y-1', wrap ? 'w-full min-w-0' : 'w-max')}>
	<dt class="text-xs-plus leading-5 text-muted-foreground">{m.common_current()}</dt>
	<dd class="min-w-0"><DigestCell value={current} {wrap} /></dd>
	<dt class="text-xs-plus leading-5 text-muted-foreground">{m.image_update_latest_label()}</dt>
	<dd class="min-w-0"><DigestCell value={latest} {wrap} /></dd>
</dl>

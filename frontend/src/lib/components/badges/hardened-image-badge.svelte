<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { HardenedImageInfo } from '#lib/types/image.js';
	import { hardenedImageDisplay } from '#lib/utils/hardened-images.js';

	let { info }: { info: HardenedImageInfo } = $props();

	const display = $derived(hardenedImageDisplay(info));
</script>

<ArcaneTooltip.Root>
	<ArcaneTooltip.Trigger>
		<Badge variant={display.variant} aria-label={m.docker_hardened_image()}>
			<display.icon class="shrink-0" />
		</Badge>
	</ArcaneTooltip.Trigger>
	<ArcaneTooltip.Content>{m.docker_hardened_image()}</ArcaneTooltip.Content>
</ArcaneTooltip.Root>

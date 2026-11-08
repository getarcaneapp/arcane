<script lang="ts">
	import { Badge, type BadgeSize } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { HardenedImageInfo } from '#lib/types/image.js';
	import { hardenedImageDisplay, hardenedSupportSummary } from '#lib/utils/hardened-images.js';

	let { info, size = 'sm' }: { info: HardenedImageInfo; size?: BadgeSize } = $props();

	const display = $derived(hardenedImageDisplay(info));
	const title = $derived(
		[m.docker_hardened_image(), info.title || info.name, hardenedSupportSummary(info)].filter(Boolean).join(' · ')
	);
</script>

<Badge variant={display.variant} {size} {title}>
	<display.icon class="shrink-0" />
	{display.label}
</Badge>

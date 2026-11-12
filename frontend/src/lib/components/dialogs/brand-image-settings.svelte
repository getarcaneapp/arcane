<script lang="ts">
	import { mode } from 'mode-watcher';

	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { logoBranding } from '#lib/utils/branding.js';
	import { getApplicationLogo } from '#lib/utils/docker.js';
	import { resolveLogoColor } from '#lib/utils/theme.svelte.js';

	import BrandImageTile, { BRAND_IMAGE_MAX_SIZE_MB } from './brand-image-tile.svelte';

	const logoTypes = ['image/png', 'image/jpeg', 'image/webp', 'image/svg+xml'];

	const logoColor = $derived(resolveLogoColor(mode.current === 'dark'));
	const mark = $derived(logoBranding());
	const full = $derived(logoBranding(true));
	const faviconHash = $derived(settingsStore.current?.customFaviconHash ?? '');
	const pwaIconHash = $derived(settingsStore.current?.customPwaIconHash ?? '');

	const slots = $derived([
		{
			slot: 'mark' as const,
			label: m.icon_mark(),
			hint: m.logo_image_types(),
			previewUrl: getApplicationLogo(false, logoColor, logoColor, mark),
			custom: !!mark.custom,
			accept: logoTypes
		},
		{
			slot: 'full' as const,
			label: m.full_logo(),
			hint: m.logo_image_types(),
			previewUrl: getApplicationLogo(true, logoColor, logoColor, full),
			custom: !!full.custom,
			accept: logoTypes
		},
		{
			slot: 'favicon' as const,
			label: m.favicon(),
			hint: m.favicon_image_types(),
			previewUrl: `/api/app/images/favicon?v=${faviconHash}`,
			custom: !!faviconHash,
			accept: ['image/png', 'image/svg+xml', 'image/x-icon', 'image/vnd.microsoft.icon', '.ico']
		},
		{
			slot: 'pwa' as const,
			label: m.app_icon(),
			hint: m.app_icon_image_types(),
			previewUrl: `/api/app/images/pwa/icon-192x192.png?v=${pwaIconHash}`,
			custom: !!pwaIconHash,
			accept: ['image/png'],
			square: { min: 512, error: m.app_icon_too_small() }
		}
	]);
</script>

<div class="flex flex-col gap-3 border-b border-border/50 py-3 last:border-0">
	<div class="flex flex-col gap-1">
		<span class="text-sm font-medium text-foreground">{m.logos_and_icons()}</span>
		<span class="text-sm text-muted-foreground">{m.logos_and_icons_description({ maxSizeMb: BRAND_IMAGE_MAX_SIZE_MB })}</span>
	</div>
	<div class="grid gap-3 sm:grid-cols-2">
		{#each slots as slot (slot.slot)}
			<BrandImageTile {...slot} />
		{/each}
	</div>
</div>

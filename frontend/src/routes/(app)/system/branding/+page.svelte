<script lang="ts">
	import { mode } from 'mode-watcher';
	import { toast } from 'svelte-sonner';

	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { isDevelopmentBuild, logoBranding } from '#lib/utils/branding.js';
	import { getApplicationLogo } from '#lib/utils/docker.js';
	import { resolveLogoColor } from '#lib/utils/theme.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import BrandImageTile, { BRAND_IMAGE_MAX_SIZE_MB } from './components/brand-image-tile.svelte';

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

	let savingDevelopmentBranding = $state(false);

	async function setDevelopmentBranding(enabled: boolean) {
		savingDevelopmentBranding = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(settingsStore.save({ developmentBrandingEnabled: enabled })),
			message: m.common_update_failed({ resource: m.settings() }),
			setLoadingState: (value) => (savingDevelopmentBranding = value),
			onSuccess: () => {
				toast.success(m.common_update_success({ resource: m.settings() }));
			}
		});
	}
</script>

<SettingsPageLayout
	title={m.branding()}
	description={m.branding_description()}
	pageType="management"
	showReadOnlyTag={settingsStore.readOnly}
>
	{#snippet mainContent()}
		<SettingsSection
			title={m.logos_and_icons()}
			description={m.logos_and_icons_description({ maxSizeMb: BRAND_IMAGE_MAX_SIZE_MB })}
			variant="plain"
		>
			<div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
				{#each slots as slot (slot.slot)}
					<BrandImageTile {...slot} />
				{/each}
			</div>
		</SettingsSection>
		{#if isDevelopmentBuild()}
			<SettingsSection class="mt-6">
				<SettingsRow
					for="developmentBrandingEnabled"
					label={m.development_branding()}
					description={m.development_branding_description()}
					layout="switch"
					lock={settingsStore.lock('developmentBrandingEnabled')}
				>
					{#snippet children({ describedBy })}
						<Switch
							id="developmentBrandingEnabled"
							checked={settingsStore.current?.developmentBrandingEnabled !== false}
							disabled={settingsStore.readOnly || savingDevelopmentBranding}
							onCheckedChange={(enabled) => void setDevelopmentBranding(enabled)}
							aria-describedby={describedBy}
						/>
					{/snippet}
				</SettingsRow>
			</SettingsSection>
		{/if}
	{/snippet}
</SettingsPageLayout>

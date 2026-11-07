<script lang="ts">
	import { mode } from 'mode-watcher';

	import { m } from '#lib/paraglide/messages.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { cn } from '#lib/utils.js';
	import { usesDevelopmentBranding } from '#lib/utils/branding.js';
	import { getApplicationLogo } from '#lib/utils/docker.js';
	import { resolveLogoColor } from '#lib/utils/theme.svelte.js';

	let { isCollapsed }: { isCollapsed: boolean } = $props();

	const logoColor = $derived(resolveLogoColor(mode.current === 'dark'));
	const animationsEnabled = $derived(userStore.current?.preferences?.animationsEnabled ?? true);
	const developmentBranding = $derived(usesDevelopmentBranding());
	const logoUrl = $derived(
		getApplicationLogo(!isCollapsed, logoColor, logoColor, { animated: animationsEnabled, development: developmentBranding })
	);
	const markSize = $derived(developmentBranding ? 32 : 24);
	const markClass = $derived(markSize === 32 ? 'size-8' : 'size-6');
</script>

<div
	class={cn('flex h-11 shrink-0 items-center transition-all duration-300', isCollapsed ? 'justify-center' : 'justify-start pl-2')}
>
	<div class={cn('relative flex shrink-0 items-center justify-center', isCollapsed ? '' : 'flex-col')}>
		<img
			src={logoUrl}
			alt={m.layout_title()}
			class={cn('drop-shadow-sm transition-all duration-300', isCollapsed ? markClass : 'h-7 w-auto max-w-40')}
			width={isCollapsed ? markSize : 160}
			height={isCollapsed ? markSize : 72}
		/>
	</div>
</div>

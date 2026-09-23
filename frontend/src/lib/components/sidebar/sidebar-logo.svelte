<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getApplicationLogo } from '#lib/utils/docker.js';
	import { resolveLogoColor } from '#lib/utils/theme.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { mode } from 'mode-watcher';

	let { isCollapsed }: { isCollapsed: boolean } = $props();

	const logoColor = $derived(resolveLogoColor(mode.current === 'dark'));
	const animationsEnabled = $derived(userStore.current?.preferences?.animationsEnabled ?? true);
	const logoUrl = $derived(getApplicationLogo(!isCollapsed, logoColor, logoColor, { animated: animationsEnabled }));
</script>

<div
	class={cn(
		'flex border-b border-border/30 transition-all duration-300',
		isCollapsed ? 'h-16 items-center justify-center px-2' : 'h-14 items-center justify-center px-4'
	)}
>
	<div class={cn('relative flex shrink-0 items-center justify-center', isCollapsed ? '' : 'flex-col')}>
		<img
			src={logoUrl}
			alt={m.layout_title()}
			class={cn('drop-shadow-sm transition-all duration-300', isCollapsed ? 'h-6 w-6' : 'h-9 w-auto max-w-40')}
			width={isCollapsed ? '24' : '160'}
			height={isCollapsed ? '24' : '72'}
		/>
	</div>
</div>

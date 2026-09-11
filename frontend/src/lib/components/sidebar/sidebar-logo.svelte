<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import { getApplicationLogo } from '#lib/utils/docker.js';
	import { accentColorPreview } from '#lib/utils/theme.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';

	let { isCollapsed }: { isCollapsed: boolean } = $props();

	const accentColor = $derived(accentColorPreview.current);
	const animationsEnabled = $derived(userStore.current?.preferences?.animationsEnabled ?? true);
	const logoUrl = $derived(getApplicationLogo(!isCollapsed, accentColor, accentColor, { animated: animationsEnabled }));
</script>

<div
	class={cn(
		'flex border-b border-border/30 transition-[height,padding] duration-300',
		isCollapsed ? 'h-16 items-center justify-center px-2' : 'h-14 items-center justify-center px-4'
	)}
>
	<div class={cn('relative flex shrink-0 items-center justify-center', isCollapsed ? '' : 'flex-col')}>
		<img
			src={logoUrl}
			alt={m.layout_title()}
			class={cn('drop-shadow-sm transition-[height,width] duration-300', isCollapsed ? 'h-6 w-6' : 'h-9 w-auto max-w-[160px]')}
			width={isCollapsed ? '24' : '160'}
			height={isCollapsed ? '24' : '72'}
		/>
	</div>
</div>

<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { getMobileNavigation } from '#lib/utils/navigation.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { IsTablet } from '#lib/hooks/is-tablet.svelte.js';
	import { cn } from '#lib/utils.js';

	interface Props {
		hasChanges: boolean | null;
		isLoading: boolean;
		onSave: () => void | Promise<void>;
		onReset: () => void;
	}

	let { hasChanges, isLoading, onSave, onReset }: Props = $props();

	const isMobile = new IsMobile();
	const isTablet = new IsTablet();
	const navigation = getMobileNavigation();
	const navigationSettings = $derived(navigation.settings);
	const navigationMode = $derived(navigationSettings.mode);
	const scrollToHideEnabled = $derived(navigationSettings.scrollToHide);
</script>

{#if isMobile.current || isTablet.current}
	<div
		class="fixed right-4 z-[var(--arcane-z-app-chrome)] flex flex-col gap-3 transition-[bottom] duration-300 ease-out sm:hidden"
		style="bottom: {scrollToHideEnabled && !navigation.visible
			? '1rem'
			: 'calc(var(--mobile-' +
				navigationMode +
				'-nav-offset, ' +
				(navigationMode === 'docked' ? 'calc(3.5rem + env(safe-area-inset-bottom))' : '6rem') +
				') + 1rem)'};"
	>
		{#if hasChanges}
			<ArcaneButton
				action="restart"
				tone="outline"
				size="lg"
				onclick={onReset}
				disabled={isLoading}
				class={cn('size-14 rounded-full border-2 shadow-lg', 'bg-background/80 backdrop-blur-md')}
				showLabel={false}
			/>
		{/if}

		<ArcaneButton
			action="save"
			onclick={onSave}
			disabled={isLoading || !hasChanges}
			loading={isLoading}
			size="lg"
			class="size-14 rounded-full shadow-lg"
			showLabel={false}
		/>

		<!-- Status indicator for mobile -->
		{#if hasChanges}
			<div class="absolute -top-2 -left-2 size-3 animate-pulse rounded-full bg-orange-500"></div>
		{/if}
	</div>
{/if}

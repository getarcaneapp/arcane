<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { cn } from '#lib/utils.js';
	import { getMobileNavigation } from '#lib/utils/navigation.js';

	interface Props {
		hasChanges: boolean | null;
		isLoading: boolean;
		onSave: () => void | Promise<void>;
		onReset: () => void;
	}

	let { hasChanges, isLoading, onSave, onReset }: Props = $props();

	const isMobile = new IsMobile();
	const navigation = getMobileNavigation();
</script>

{#if isMobile.current}
	<div
		class="fixed right-4 bottom-(--fab-bottom) z-(--arcane-z-app-chrome) flex flex-col gap-3 transition-all duration-300 ease-out"
		style="--fab-bottom: {navigation.visible ? 'calc(var(--mobile-floating-nav-offset, 6rem) + 1rem)' : '1rem'};"
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

		{#if hasChanges}
			<div class="absolute -top-2 -left-2 size-3 animate-pulse rounded-full bg-warning"></div>
		{/if}
	</div>
{/if}

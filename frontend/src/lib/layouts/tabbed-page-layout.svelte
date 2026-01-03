<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar/index.js';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import { ArrowLeftIcon } from '$lib/icons';

	interface Props {
		backUrl: string;
		backLabel: string;
		tabItems: TabItem[];
		selectedTab: string;
		onTabChange: (value: string) => void;
		headerInfo: Snippet;
		headerActions?: Snippet;
		subHeader?: Snippet;
		tabContent: Snippet<[string]>;
		class?: string;
		showFloatingHeader?: boolean;
	}

	let {
		backUrl,
		backLabel,
		tabItems,
		selectedTab,
		onTabChange,
		headerInfo,
		headerActions,
		subHeader,
		tabContent,
		class: className = '',
		showFloatingHeader = false
	}: Props = $props();

	let scrollContainer = $state<HTMLDivElement | null>(null);

	$effect(() => {
		const getScrollTop = () => (scrollContainer ? scrollContainer.scrollTop : window.scrollY);
		const onScroll = () => {
			showFloatingHeader = getScrollTop() > 100;
		};

		const target: Window | HTMLDivElement = scrollContainer ?? window;
		target.addEventListener('scroll', onScroll as EventListener);
		onScroll();
		return () => target.removeEventListener('scroll', onScroll as EventListener);
	});
</script>

<div class={cn('bg-background flex h-full min-h-0 flex-col', className)}>
	<Tabs.Root value={selectedTab} class="flex min-h-0 w-full flex-1 flex-col">
		<div
			class="sticky top-0 border-b transition-all duration-300"
			style="opacity: {showFloatingHeader ? 0 : 1}; pointer-events: {showFloatingHeader ? 'none' : 'auto'};"
		>
			<div class="max-w-full px-4 py-3">
				<div class="flex items-start justify-between gap-3">
					<div class="flex min-w-0 items-start gap-3">
						<ArcaneButton action="base" tone="ghost" size="sm" href={backUrl}>
							<ArrowLeftIcon class="size-4" />
							{backLabel}
						</ArcaneButton>
						<div class="min-w-0">
							{@render headerInfo()}
						</div>
					</div>
					{#if headerActions}
						{@render headerActions()}
					{/if}
				</div>

				{#if subHeader}
					{@render subHeader()}
				{/if}

				<div class="mt-4">
					<TabBar items={tabItems} value={selectedTab} onValueChange={onTabChange} />
				</div>
			</div>
		</div>

		{#if showFloatingHeader}
			<div class="fixed top-4 left-1/2 z-30 -translate-x-1/2 transition-all duration-300 ease-in-out">
				<div
					class="bg-popover/90 supports-backdrop-filter:bg-popover/80 bubble-shadow-lg border-border/50 rounded-xl border px-4 py-3 backdrop-blur-md"
				>
					<div class="flex items-center gap-4">
						<div class="min-w-0">
							{@render headerInfo()}
						</div>
						{#if headerActions}
							<div class="bg-border h-4 w-px"></div>
							{@render headerActions()}
						{/if}
					</div>
				</div>
			</div>
		{/if}

		<div class="min-h-0 flex-1 overflow-y-auto" bind:this={scrollContainer}>
			<div class="flex h-full min-h-0 flex-col px-1 py-4 pb-2 sm:px-4">
				{@render tabContent(selectedTab)}
			</div>
		</div>
	</Tabs.Root>
</div>

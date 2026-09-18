<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils.js';
	import { ArrowLeftIcon } from '#lib/icons/index.js';

	interface Props {
		backUrl?: string;
		backLabel?: string;
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
		showFloatingHeader
	}: Props = $props();

	let scrollTop = $state(0);
	const floatingHeaderVisible = $derived(showFloatingHeader ?? scrollTop > 100);
</script>

<div class={cn('flex h-full min-h-0 flex-col bg-background', className)}>
	<Tabs.Root value={selectedTab} class="flex min-h-0 w-full flex-1 flex-col">
		<div
			class="sticky top-0 border-b transition-opacity duration-300"
			style="opacity: {floatingHeaderVisible ? 0 : 1}; pointer-events: {floatingHeaderVisible ? 'none' : 'auto'};"
			inert={floatingHeaderVisible || undefined}
		>
			<div class="max-w-full px-4 py-3">
				<div class="flex items-start gap-3">
					<div class="flex max-w-[70%] min-w-0 items-start gap-2">
						{#if backUrl}
							<ArcaneButton action="base" tone="ghost" size="sm" href={backUrl}>
								<ArrowLeftIcon class="size-4" />
								{backLabel ?? ''}
							</ArcaneButton>
						{/if}
						<div class="min-w-0">
							{@render headerInfo()}
						</div>
					</div>
					{#if headerActions}
						<!-- basis-0: the column's width never depends on its content, so the
						     self-measuring actions inside cannot feed back into their own space. -->
						<div class="flex min-w-9 flex-1 basis-0 items-center justify-end gap-2">
							{@render headerActions()}
						</div>
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

		{#if floatingHeaderVisible}
			<!-- A full-width, click-through strip centres the bubble. Positioning the bubble itself at
			     left-1/2 would cap its shrink-to-fit width at half the viewport. -->
			<div class="pointer-events-none fixed inset-x-4 top-4 z-[var(--arcane-z-page-floating)] flex justify-center">
				<div
					class="bubble-shadow-lg pointer-events-auto max-w-full rounded-lg border border-border/50 bg-popover/90 px-4 py-3 backdrop-blur-md supports-backdrop-filter:bg-popover/80"
				>
					<div class="flex items-center gap-4">
						<div class="min-w-0">
							{@render headerInfo()}
						</div>
						{#if headerActions}
							<div class="h-4 w-px shrink-0 bg-border"></div>
							<div class="flex min-w-9 flex-1 basis-0 items-center justify-end gap-2">
								{@render headerActions()}
							</div>
						{/if}
					</div>
				</div>
			</div>
		{/if}

		<div class="min-h-0 flex-1 overflow-y-auto" onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}>
			<div class="flex h-full min-h-0 flex-col px-1 py-4 pb-2 sm:px-4">
				{@render tabContent(selectedTab)}
			</div>
		</div>
	</Tabs.Root>
</div>

<script lang="ts">
	import { page } from '$app/state';
	import { cn } from '$lib/utils';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { isActiveNavItem } from '$lib/utils/navigation';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { m } from '$lib/paraglide/messages';
	import { EllipsisIcon } from '$lib/icons';

	let { items }: { items: NavigationItem[] } = $props();

	const isActive = $derived(
		items.some(
			(item) =>
				isActiveNavItem(page.url.pathname, item.url) ||
				(item.items ?? []).some((child) => isActiveNavItem(page.url.pathname, child.url))
		)
	);
</script>

{#snippet entryLink(entry: NavigationItem, indent: boolean)}
	{@const EntryIcon = entry.icon}
	<DropdownMenu.Item>
		{#snippet child({ props })}
			<a href={entry.url} {...props} class={cn(props['class'] as string, indent && 'pl-7')}>
				<EntryIcon class="text-muted-foreground size-4 shrink-0" />
				<span>{entry.title}</span>
			</a>
		{/snippet}
	</DropdownMenu.Item>
{/snippet}

<DropdownMenu.Root>
	<DropdownMenu.Trigger
		class={cn(
			'flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-sm font-medium whitespace-nowrap transition-colors',
			isActive
				? 'bg-sidebar-accent text-sidebar-accent-foreground'
				: 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground data-[state=open]:bg-sidebar-accent/60 data-[state=open]:text-foreground'
		)}
		aria-label={m.header_nav_more()}
	>
		<EllipsisIcon class="size-4 shrink-0" />
		<span>{m.header_nav_more()}</span>
	</DropdownMenu.Trigger>
	<DropdownMenu.Content
		align="end"
		sideOffset={8}
		class="border-border/30 min-w-52 rounded-xl border p-1.5 shadow-lg backdrop-blur-2xl backdrop-saturate-150"
	>
		{#each items as item, index (item.url)}
			{#if index > 0 && (item.items?.length ?? 0) > 0}
				<DropdownMenu.Separator class="my-1" />
			{/if}
			{@render entryLink(item, false)}
			{#each item.items ?? [] as child (child.url)}
				{@render entryLink(child, true)}
			{/each}
		{/each}
	</DropdownMenu.Content>
</DropdownMenu.Root>

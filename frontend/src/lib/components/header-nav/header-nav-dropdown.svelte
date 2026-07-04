<script lang="ts">
	import { page } from '$app/state';
	import { cn } from '$lib/utils';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { isActiveNavItem } from '$lib/utils/navigation';
	import type { NavigationItem } from '$lib/config/navigation-config';
	import { ArrowDownIcon } from '$lib/icons';

	let {
		item,
		parentLink = true,
		measureOnly = false
	}: {
		item: NavigationItem;
		/** include the parent page itself as the first dropdown entry */
		parentLink?: boolean;
		/** render only the styled trigger contents, for the hidden measurement row */
		measureOnly?: boolean;
	} = $props();

	const Icon = $derived(item.icon);
	const isActive = $derived(
		isActiveNavItem(page.url.pathname, item.url) ||
			(item.items ?? []).some((child) => isActiveNavItem(page.url.pathname, child.url))
	);
	const entries = $derived(parentLink ? [{ ...item, items: undefined }, ...(item.items ?? [])] : (item.items ?? []));

	const triggerClass = $derived(
		cn(
			'flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-sm font-medium whitespace-nowrap transition-colors',
			isActive
				? 'bg-sidebar-accent text-sidebar-accent-foreground'
				: 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground data-[state=open]:bg-sidebar-accent/60 data-[state=open]:text-foreground'
		)
	);
</script>

{#if measureOnly}
	<!-- placeholder spans instead of icon SVGs: duplicated defs in the hidden
	     measurement row would hijack url(#id) clip-path lookups -->
	<span class={triggerClass}>
		<span class="size-4 shrink-0"></span>
		<span>{item.title}</span>
		<span class="size-3 shrink-0"></span>
	</span>
{:else}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger class={triggerClass}>
			<Icon class="size-4 shrink-0" />
			<span>{item.title}</span>
			<ArrowDownIcon class="text-muted-foreground/70 size-3 shrink-0" />
		</DropdownMenu.Trigger>
		<DropdownMenu.Content
			align="start"
			sideOffset={8}
			class="border-border/30 min-w-52 rounded-xl border p-1.5 shadow-lg backdrop-blur-2xl backdrop-saturate-150"
		>
			{#each entries as entry (entry.url)}
				{@const EntryIcon = entry.icon}
				<DropdownMenu.Item>
					{#snippet child({ props })}
						<a href={entry.url} {...props}>
							<EntryIcon class="text-muted-foreground size-4 shrink-0" />
							<span>{entry.title}</span>
						</a>
					{/snippet}
				</DropdownMenu.Item>
			{/each}
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/if}

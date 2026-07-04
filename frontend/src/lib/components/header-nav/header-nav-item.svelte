<script lang="ts">
	import { page } from '$app/state';
	import { cn } from '$lib/utils';
	import { isActiveNavItem } from '$lib/utils/navigation';
	import type { NavigationItem } from '$lib/config/navigation-config';

	let {
		item,
		measureOnly = false
	}: {
		item: NavigationItem;
		/** render a placeholder instead of the icon SVG — duplicated icon defs
		 * (clip-paths) inside the hidden measurement row hijack url(#id) lookups
		 * and blank out the visible icons */
		measureOnly?: boolean;
	} = $props();

	const Icon = $derived(item.icon);
	const isActive = $derived(isActiveNavItem(page.url.pathname, item.url));
	const itemClass = $derived(
		cn(
			'flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-sm font-medium whitespace-nowrap transition-colors',
			isActive
				? 'bg-sidebar-accent text-sidebar-accent-foreground'
				: 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground'
		)
	);
</script>

{#if measureOnly}
	<span class={itemClass}>
		<span class="size-4 shrink-0"></span>
		<span>{item.title}</span>
	</span>
{:else}
	<a href={item.url} aria-current={isActive ? 'page' : undefined} class={itemClass}>
		<Icon class="size-4 shrink-0" />
		<span>{item.title}</span>
	</a>
{/if}

<script lang="ts" module>
	import type { VariantProps } from 'tailwind-variants';

	import { tv, cn, type WithElementRef, type WithoutChildrenOrChild } from '#lib/utils.js';

	export const sidebarMenuButtonVariants = tv({
		base: 'peer/menu-button outline-hidden ring-sidebar-ring hover:bg-sidebar-accent hover:text-sidebar-accent-foreground active:bg-sidebar-accent active:text-sidebar-accent-foreground group-has-data-[sidebar=menu-action]/menu-item:pr-9 data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground data-[state=open]:hover:bg-sidebar-accent data-[state=open]:hover:text-sidebar-accent-foreground group-data-[collapsible=icon]:!size-9.5 group-data-[collapsible=icon]:!p-0 flex w-full items-center gap-3 overflow-hidden rounded-md pr-3 pl-2.5 text-left text-sm-plus font-medium transition-[width,height,padding] focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50 data-[active=true]:font-semibold [&>span:last-child]:truncate [&>svg]:size-4.5 [&>svg]:shrink-0 [&>svg]:flex-shrink-0 group-data-[collapsible=icon]:[&>span]:hidden group-data-[collapsible=icon]:[&>svg.ml-auto]:hidden group-data-[collapsible=icon]:[&>div.grid]:hidden [&>a]:flex [&>a]:w-full [&>a]:items-center [&>a]:gap-3 [&>button]:flex [&>button]:w-full [&>button]:items-center [&>button]:gap-3 group-data-[collapsible=icon]:[&>a]:w-auto group-data-[collapsible=icon]:[&>button]:w-auto',
		variants: {
			variant: {
				default: 'hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
				outline:
					'bg-background hover:bg-sidebar-accent hover:text-sidebar-accent-foreground shadow-[0_0_0_1px_var(--sidebar-border)] hover:shadow-[0_0_0_1px_var(--sidebar-accent)]'
			},
			size: {
				default: 'h-9.5',
				sm: 'h-7 text-xs',
				lg: 'group-data-[collapsible=icon]:p-0! h-12 text-sm'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default'
		}
	});

	export type SidebarMenuButtonVariant = VariantProps<typeof sidebarMenuButtonVariants>['variant'];
	export type SidebarMenuButtonSize = VariantProps<typeof sidebarMenuButtonVariants>['size'];
</script>

<script lang="ts">
	import { mergeProps } from 'bits-ui';
	import type { ComponentProps, Snippet } from 'svelte';
	import type { HTMLAttributes } from 'svelte/elements';

	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';

	import { useSidebar } from './context.svelte.js';

	let {
		ref = $bindable(null),
		class: className,
		children,
		child,
		variant = 'default',
		size = 'default',
		isActive = false,
		tooltipContent,
		tooltipContentProps,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLButtonElement>, HTMLButtonElement> & {
		isActive?: boolean;
		variant?: SidebarMenuButtonVariant;
		size?: SidebarMenuButtonSize;
		tooltipContent?: Snippet | string;
		tooltipContentProps?: WithoutChildrenOrChild<ComponentProps<typeof ArcaneTooltip.Content>>;
		child?: Snippet<[{ props: Record<string, unknown> }]>;
	} = $props();

	const sidebar = useSidebar();

	const buttonProps = $derived({
		class: cn(
			sidebarMenuButtonVariants({ variant, size }),
			// Always ensure icons are visible and centered in collapsed mode
			'group-data-[collapsible=icon]:!justify-center',
			sidebar.hoverExpansionEnabled && [
				'group-data-[collapsible=icon]:group-data-[hovered=true]:!pr-3 group-data-[collapsible=icon]:group-data-[hovered=true]:!pl-2.5',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:!w-full group-data-[collapsible=icon]:group-data-[hovered=true]:!justify-start',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:[&>a]:!w-full group-data-[collapsible=icon]:group-data-[hovered=true]:[&>a]:!justify-start',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:[&>button]:!w-full group-data-[collapsible=icon]:group-data-[hovered=true]:[&>button]:!justify-start',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:[&>span]:!block',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:[&>div.grid]:!grid',
				'group-data-[collapsible=icon]:group-data-[hovered=true]:[&>svg.ml-auto]:!block',
				size === 'lg' &&
					'group-data-[collapsible=icon]:group-data-[hovered=true]:!h-12 group-data-[collapsible=icon]:group-data-[hovered=true]:!p-2'
			],
			className
		),
		'data-slot': 'sidebar-menu-button',
		'data-sidebar': 'menu-button',
		'data-size': size,
		'data-active': isActive,
		...restProps
	});
</script>

{#snippet Button({ props }: { props?: Record<string, unknown> })}
	{@const mergedProps = mergeProps(buttonProps, props)}
	{#if child}
		{@render child({ props: mergedProps })}
	{:else}
		<button bind:this={ref} {...mergedProps}>
			{@render children?.()}
		</button>
	{/if}
{/snippet}

{#if !tooltipContent}
	{@render Button({})}
{:else}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				{@render Button({ props })}
			{/snippet}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content side="right" align="center" class="z-(--arcane-z-surface)" {...tooltipContentProps}>
			{#if typeof tooltipContent === 'string'}
				{tooltipContent}
			{:else if tooltipContent}
				{@render tooltipContent()}
			{/if}
		</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/if}

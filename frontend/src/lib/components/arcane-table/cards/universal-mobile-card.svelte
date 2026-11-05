<script lang="ts" generics="T">
	import type { Snippet, Component } from 'svelte';

	import TableCheckbox from '#lib/components/arcane-table/arcane-table-checkbox.svelte';
	import { getMobileRowSelection, hasMobileRowSelection } from '#lib/components/arcane-table/arcane-table.types.svelte.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';
	import { rowKey } from '#lib/utils/environments.js';

	import { iconBgClass, iconTextClass, type IconVariant } from './icon-variants.js';
	import ResourceCardField from './resource-card-field.svelte';
	import ResourceCardMeta from './resource-card-meta.svelte';
	import ResourceCard from './resource-card.svelte';

	type BadgeVariant = 'green' | 'red' | 'amber' | 'blue' | 'purple' | 'gray' | 'orange';

	interface IconConfig {
		component: Component<any>;
		variant: IconVariant;
		imageUrl?: string;
		alt?: string;
	}

	interface BadgeConfig {
		variant: BadgeVariant;
		text: string;
	}

	type FieldType = 'text' | 'badge' | 'date' | 'mono' | 'component';

	interface FieldDefinition<T> {
		label: string;
		getValue: (item: T) => any;
		type?: FieldType;
		icon?: Component<any>;
		iconVariant?: IconVariant;
		badgeVariant?: BadgeVariant;
		component?: Snippet<[value: any]>;
		layout?: 'inline' | 'stacked';
		show?: boolean;
	}

	interface FooterConfig<T> {
		label: string;
		getValue: (item: T) => string;
		icon: Component<any>;
	}

	let {
		item,
		icon,
		title,
		titleContent,
		subtitle,
		badges = [],
		fields = [],
		footer,
		rowActions,
		expander,
		children,
		class: className = '',
		href,
		onclick
	}: {
		item: T;
		icon: IconConfig | ((item: T) => IconConfig);
		title: (item: T) => string;
		titleContent?: Snippet<[item: T]>;
		subtitle?: (item: T) => string | null;
		badges?: (BadgeConfig | ((item: T) => BadgeConfig | null))[];
		fields?: FieldDefinition<T>[];
		footer?: FooterConfig<T>;
		rowActions?: Snippet<[{ item: T }]>;
		expander?: Snippet;
		children?: Snippet;
		class?: string;
		href?: (item: T) => string | undefined;
		onclick?: (item: T) => void;
	} = $props();

	const selection = hasMobileRowSelection() ? getMobileRowSelection() : undefined;
	const rowId = $derived(rowKey(item as { id: string; environmentId?: string; environmentTagged?: boolean }));
	const selectable = $derived(selection?.enabled ?? false);
	const selected = $derived(selectable && !!selection?.isSelected(rowId));

	const resolvedIcon = $derived(typeof icon === 'function' ? icon(item) : icon);
	const resolvedBadges = $derived(
		badges.map((b) => (typeof b === 'function' ? b(item) : b)).filter((b): b is BadgeConfig => b !== null)
	);

	let errorImageUrl = $state<string | undefined>(undefined);
	const validImageUrl = $derived(
		resolvedIcon?.imageUrl && resolvedIcon.imageUrl !== errorImageUrl ? resolvedIcon.imageUrl : null
	);

	const environmentId = $derived((item as { environmentId?: string }).environmentId);
	const visibleFields = $derived(fields.filter((f) => f.show !== false));
	const subtitleValue = $derived(subtitle?.(item));
	const footerValue = $derived(footer?.getValue(item));
	const hrefValue = $derived(href?.(item));
	const hasFields = $derived(visibleFields.some((field) => isPresent(field.getValue(item))));

	function isPresent(value: unknown): boolean {
		return value !== null && value !== undefined;
	}
</script>

{#snippet FieldValue(field: FieldDefinition<T>, value: any)}
	{#if field.type === 'badge' && field.badgeVariant}
		<Badge variant={field.badgeVariant} size="sm" minWidth="20">{String(value)}</Badge>
	{:else if field.type === 'mono'}
		<span class="truncate font-mono text-xs">{value}</span>
	{:else if field.type === 'component' && field.component}
		{@render field.component(value)}
	{:else}
		<span class="min-w-0 truncate">{value}</span>
	{/if}
{/snippet}

{#snippet cardTitle()}
	<div class="flex min-w-0 items-center gap-3">
		{#if resolvedIcon}
			{@const IconComponent = resolvedIcon.component}
			<div
				class={cn(
					'flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 ring-foreground/5 ring-inset',
					iconBgClass[resolvedIcon.variant]
				)}
			>
				{#if validImageUrl}
					<img
						src={validImageUrl}
						alt={resolvedIcon.alt ?? ''}
						loading="lazy"
						decoding="async"
						referrerpolicy="no-referrer"
						class="size-5 object-contain"
						onerror={() => (errorImageUrl = resolvedIcon?.imageUrl)}
					/>
				{:else}
					<IconComponent class={cn(iconTextClass[resolvedIcon.variant], 'size-4.5')} />
				{/if}
			</div>
		{/if}
		<div class="min-w-0 flex-1">
			{#if titleContent}
				<h3 class="flex min-w-0 text-sm leading-tight font-medium">
					{#if hrefValue}
						<a
							href={hrefValue}
							aria-label={title(item)}
							class="stretched-link flex min-w-0 rounded-sm hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
							>{@render titleContent(item)}</a
						>
					{:else}
						{@render titleContent(item)}
					{/if}
				</h3>
			{:else}
				<h3 class="line-clamp-2 text-sm leading-tight font-medium wrap-anywhere" title={title(item)}>
					{#if hrefValue}
						<a
							href={hrefValue}
							class="stretched-link rounded-sm hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
							>{title(item)}</a
						>
					{:else}
						{title(item)}
					{/if}
				</h3>
			{/if}
			{#if subtitleValue}
				<p class="mt-0.5 truncate font-mono text-xs font-normal text-muted-foreground">{subtitleValue}</p>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet cardSelect()}
	<TableCheckbox
		checked={selected}
		onCheckedChange={(value, modifiers) => selection?.toggle(rowId, value, modifiers)}
		onclick={(event: MouseEvent) => event.stopPropagation()}
		aria-label={m.common_select_row()}
	/>
{/snippet}

{#snippet cardContext()}
	<EnvironmentCell value={environmentId} />
{/snippet}

{#snippet cardActions()}
	{@render expander?.()}
	{@render rowActions?.({ item })}
{/snippet}

{#snippet cardBadges()}
	{#each resolvedBadges as badge (`${badge.variant}:${badge.text}`)}
		<Badge variant={badge.variant} size="sm" minWidth="20">{badge.text}</Badge>
	{/each}
{/snippet}

{#snippet cardFields()}
	{#each visibleFields as field (field.label)}
		{@const value = field.getValue(item)}
		{#if isPresent(value)}
			<ResourceCardField
				label={field.label}
				icon={field.icon}
				iconClass={field.iconVariant ? iconTextClass[field.iconVariant] : undefined}
				layout={field.layout}
			>
				{@render FieldValue(field, value)}
			</ResourceCardField>
		{/if}
	{/each}
{/snippet}

{#snippet cardFooter()}
	{#if footer}
		<ResourceCardMeta label={footer.label} icon={footer.icon}>
			<span class="font-mono">{footerValue}</span>
		</ResourceCardMeta>
	{/if}
{/snippet}

<div class={cn('w-full px-2 py-1.5', className)}>
	<ResourceCard
		{selected}
		select={selectable ? cardSelect : undefined}
		onclick={onclick ? () => onclick(item) : undefined}
		class={hrefValue ? 'stretched-link-host' : undefined}
		title={cardTitle}
		context={environmentId ? cardContext : undefined}
		actions={rowActions || expander ? cardActions : undefined}
		badges={resolvedBadges.length > 0 ? cardBadges : undefined}
		fields={hasFields ? cardFields : undefined}
		footer={footerValue ? cardFooter : undefined}
	>
		{@render children?.()}
	</ResourceCard>
</div>

<script lang="ts" generics="T">
	import type { Snippet, Component } from 'svelte';

	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { cn } from '#lib/utils.js';

	import ResourceCardField from './resource-card-field.svelte';
	import ResourceCardMeta from './resource-card-meta.svelte';
	import ResourceCard from './resource-card.svelte';

	type IconVariant = 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'gray' | 'sky' | 'orange';
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
		subtitle,
		badges = [],
		fields = [],
		footer,
		rowActions,
		children,
		class: className = '',
		onclick
	}: {
		item: T;
		icon: IconConfig | ((item: T) => IconConfig);
		title: (item: T) => string;
		subtitle?: (item: T) => string | null;
		badges?: (BadgeConfig | ((item: T) => BadgeConfig | null))[];
		fields?: FieldDefinition<T>[];
		footer?: FooterConfig<T>;
		rowActions?: Snippet<[{ item: T }]>;
		children?: Snippet;
		class?: string;
		onclick?: (item: T) => void;
	} = $props();

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
	const hasFields = $derived(visibleFields.some((field) => isPresent(field.getValue(item))));

	function isPresent(value: unknown): boolean {
		return value !== null && value !== undefined;
	}

	function getIconBgClass(variant: IconVariant): string {
		const map: Record<IconVariant, string> = {
			emerald: 'bg-success/8',
			red: 'bg-destructive/8',
			amber: 'bg-warning/8',
			blue: 'bg-info/8',
			purple: 'bg-purple/8',
			gray: 'bg-muted/40',
			sky: 'bg-sky/8',
			orange: 'bg-orange/8'
		};
		return map[variant];
	}

	function getIconTextClass(variant: IconVariant): string {
		const map: Record<IconVariant, string> = {
			emerald: 'text-success',
			red: 'text-destructive',
			amber: 'text-warning',
			blue: 'text-info',
			purple: 'text-purple',
			gray: 'text-muted-foreground',
			sky: 'text-sky',
			orange: 'text-orange'
		};
		return map[variant];
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
					getIconBgClass(resolvedIcon.variant)
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
					<IconComponent class={cn(getIconTextClass(resolvedIcon.variant), 'size-4.5')} />
				{/if}
			</div>
		{/if}
		<div class="min-w-0 flex-1">
			<h3 class="truncate text-sm leading-tight font-medium" title={title(item)}>{title(item)}</h3>
			{#if subtitleValue}
				<p class="mt-0.5 truncate font-mono text-xs font-normal text-muted-foreground">{subtitleValue}</p>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet cardContext()}
	<EnvironmentCell value={environmentId} />
{/snippet}

{#snippet cardActions()}
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
				iconClass={field.iconVariant ? getIconTextClass(field.iconVariant) : undefined}
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

<div class={cn('w-full px-3 py-2', className)}>
	<ResourceCard
		onclick={onclick ? () => onclick(item) : undefined}
		title={cardTitle}
		context={environmentId ? cardContext : undefined}
		actions={rowActions ? cardActions : undefined}
		badges={resolvedBadges.length > 0 ? cardBadges : undefined}
		fields={hasFields ? cardFields : undefined}
		footer={footerValue ? cardFooter : undefined}
	>
		{#if children}
			<div class="px-3.5 pb-3">
				{@render children()}
			</div>
		{/if}
	</ResourceCard>
</div>

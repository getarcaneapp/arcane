<script lang="ts">
	import type { Snippet } from 'svelte';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { InfoIcon, type IconType } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		label: string;
		/** `null` renders a dash for values that are unavailable rather than zero. */
		value: string | number | null;
		unavailableLabel?: string;
		icon?: IconType;
		help?: string;
		href?: string;
		loading?: boolean;
		/** Context line rendered under the value. */
		children?: Snippet;
	}

	let {
		label,
		value,
		unavailableLabel = m.common_unavailable(),
		icon: Icon,
		help,
		href,
		loading = false,
		children
	}: Props = $props();
</script>

{#snippet labelContent()}
	{#if Icon}
		<Icon class="size-3.5 shrink-0" />
	{/if}
	{label}
{/snippet}

<div class="group relative flex min-w-0 flex-col items-center px-4 text-center">
	<dt
		class="flex items-center justify-center gap-1.5 text-sm text-muted-foreground transition-colors group-hover:text-foreground"
	>
		{#if href}
			<!-- The link's overlay makes the whole tile clickable. -->
			<a
				{href}
				class="flex items-center gap-1.5 after:absolute after:inset-0 after:rounded-sm focus-visible:outline-none focus-visible:after:outline-2 focus-visible:after:outline-offset-4 focus-visible:after:outline-ring"
			>
				{@render labelContent()}
			</a>
		{:else}
			{@render labelContent()}
		{/if}
		{#if help}
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger class="relative z-10">
					<span class="inline-flex cursor-help items-center" aria-label={label}>
						<InfoIcon class="size-3.5 shrink-0" />
					</span>
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content class="max-w-64">
					<p class="mb-1 text-sm font-medium">{label}</p>
					<p class="text-xs">{help}</p>
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		{/if}
	</dt>
	<dd class="mt-2 flex flex-col items-center gap-1">
		{#if loading}
			<Skeleton class="h-9 w-14" />
			<Skeleton class="h-3.5 w-24" />
		{:else}
			{#if value === null}
				<span class="text-3xl font-semibold text-muted-foreground" aria-label={unavailableLabel}>–</span>
			{:else}
				<span class="text-3xl font-semibold tabular-nums">{value}</span>
			{/if}
			{#if children}
				<div class="text-xs text-muted-foreground tabular-nums">{@render children()}</div>
			{/if}
		{/if}
	</dd>
</div>

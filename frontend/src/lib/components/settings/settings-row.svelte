<script lang="ts">
	import type { Snippet } from 'svelte';

	import { SettingLock } from '#lib/components/badges/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { AlertIcon, LayersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { SettingState } from '#lib/types/settings.js';
	import type { MixedSettingValue } from '#lib/types/ui/settings.js';
	import { cn } from '#lib/utils.js';

	import MixedValuesPopover from './mixed-values-popover.svelte';

	interface Props {
		label: string;
		description?: string;
		helpText?: string | Snippet;
		warningText?: string;
		error?: string | null;
		for?: string;
		layout?: 'default' | 'switch' | 'wide';
		labelExtra?: Snippet;
		lock?: SettingState;
		mixed?: MixedSettingValue[];
		expanded?: Snippet;
		contentClass?: string;
		class?: string;
		children: Snippet<[{ describedBy: string | undefined; errorId: string | undefined }]>;
	}

	let {
		label,
		description,
		helpText,
		warningText,
		error,
		for: htmlFor,
		layout = 'default',
		labelExtra,
		lock,
		mixed,
		expanded,
		contentClass,
		class: className,
		children
	}: Props = $props();

	const uid = $props.id();
	const describedBy = $derived(
		[description && `${uid}-description`, helpText && `${uid}-help`, error && `${uid}-error`].filter(Boolean).join(' ') ||
			undefined
	);
</script>

{#snippet notes()}
	{#if error}
		<p id="{uid}-error" class="mt-1.5 text-xs font-medium text-destructive">{error}</p>
	{/if}
	{#if typeof helpText === 'function'}
		<div id="{uid}-help" class="mt-1.5 text-xs text-muted-foreground">
			{@render helpText()}
		</div>
	{:else if helpText}
		<p id="{uid}-help" class="mt-1.5 text-xs text-muted-foreground">{helpText}</p>
	{/if}
	{#if warningText}
		<Alert.Root variant="warning-subtle" size="sm" class="mt-2 w-fit max-w-full">
			<AlertIcon class="size-4" />
			<Alert.Description>{warningText}</Alert.Description>
		</Alert.Root>
	{/if}
{/snippet}

<div class={cn('flex flex-col gap-4 px-5 py-4', className)}>
	<div
		class={cn(
			'grid gap-x-8 gap-y-2',
			layout === 'default' && 'md:grid-cols-settings-row md:items-start',
			layout === 'switch' && 'grid-cols-content-action items-center'
		)}
	>
		<div class="min-w-0">
			<div class="flex flex-wrap items-center gap-2">
				<Label for={htmlFor} weight="semibold" leading="snug">{label}</Label>
				{#if lock?.locked}
					<SettingLock {lock} />
				{/if}
				{#if mixed}
					<MixedValuesPopover
						values={mixed}
						class="rounded-full focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
					>
						<Badge variant="gray">
							<LayersIcon />
							{m.mixed()}
						</Badge>
					</MixedValuesPopover>
				{/if}
			</div>
			{#if description}
				<p id="{uid}-description" class="mt-0.5 max-w-settings-description text-xs-plus leading-5 text-muted-foreground">
					{description}
				</p>
			{/if}
			{#if labelExtra}
				{@render labelExtra()}
			{/if}
			{#if layout === 'switch'}
				{@render notes()}
			{/if}
		</div>
		<div class={cn('min-w-0', layout === 'switch' && 'flex justify-end', contentClass)}>
			<fieldset disabled={lock?.locked} class="contents">
				{@render children({ describedBy, errorId: error ? `${uid}-error` : undefined })}
			</fieldset>
			{#if layout !== 'switch'}
				{@render notes()}
			{/if}
		</div>
	</div>
	{#if expanded}
		<div class="flex flex-col gap-4 border-l-2 border-border/60 pl-5">
			{@render expanded()}
		</div>
	{/if}
</div>

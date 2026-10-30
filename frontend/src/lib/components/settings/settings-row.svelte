<script lang="ts">
	import type { Snippet } from 'svelte';

	import { SettingLock } from '#lib/components/badges/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import { AlertTriangleIcon, ArrowRightIcon, LayersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { MixedSettingValue, SettingState } from '#lib/types/settings.js';
	import { cn } from '#lib/utils.js';

	// One setting: label and description on the left, the control on the right.
	// `switch` keeps a compact right-aligned control; `wide` drops the control under the label.
	interface Props {
		label: string;
		description?: string;
		helpText?: string | Snippet;
		/** Caution shown as a small warning alert; use for side effects worth a second look. */
		warningText?: string;
		error?: string | null;
		/** Id of the control, so the label focuses it. */
		for?: string;
		layout?: 'default' | 'switch' | 'wide';
		labelExtra?: Snippet;
		lock?: SettingState;
		mixed?: MixedSettingValue[];
		/** Extra content under the row (e.g. fields revealed by a switch), indented. */
		expanded?: Snippet;
		contentClass?: string;
		class?: string;
		children: Snippet;
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
</script>

{#snippet notes()}
	{#if error}
		<p class="mt-1.5 text-xs font-medium text-destructive">{error}</p>
	{/if}
	{#if typeof helpText === 'function'}
		<div class="mt-1.5 text-xs text-muted-foreground">{@render helpText()}</div>
	{:else if helpText}
		<p class="mt-1.5 text-xs text-muted-foreground">{helpText}</p>
	{/if}
	{#if warningText}
		<Alert.Root variant="warning-subtle" size="sm" class="mt-2 w-fit max-w-full">
			<AlertTriangleIcon class="size-4" />
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
				<Label for={htmlFor}>{label}</Label>
				{#if lock?.locked}
					<SettingLock {lock} />
				{/if}
				{#if mixed}
					<Popover.Root>
						<Popover.Trigger>
							{#snippet child({ props })}
								<button
									{...props}
									type="button"
									title={m.mixed_across_selected_environments()}
									class="rounded-full focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none"
								>
									<Badge variant="gray">
										<LayersIcon />
										{m.mixed()}
									</Badge>
								</button>
							{/snippet}
						</Popover.Trigger>
						<Popover.Content
							align="start"
							variant="panel"
							class="w-80 overflow-hidden"
							onOpenAutoFocus={(event) => event.preventDefault()}
						>
							<p class="px-4 pt-3 pb-2 text-xs text-muted-foreground">{m.mixed_across_selected_environments()}</p>
							<div class="divide-y divide-border/50 border-t border-border/50">
								{#each mixed as value, index (index)}
									<Popover.Close onclick={value.apply}>
										{#snippet child({ props })}
											<button
												{...props}
												type="button"
												class="flex w-full items-center justify-between gap-4 px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
											>
												<span class="min-w-0">
													<span class="block truncate text-sm font-medium">{value.label}</span>
													<span class="block truncate text-xs text-muted-foreground">{value.environments.join(', ')}</span>
												</span>
												<span class="flex shrink-0 items-center gap-0.5 text-xs text-muted-foreground">
													{m.use_for_all()}
													<ArrowRightIcon class="size-3.5" />
												</span>
											</button>
										{/snippet}
									</Popover.Close>
								{/each}
							</div>
						</Popover.Content>
					</Popover.Root>
				{/if}
			</div>
			{#if description}
				<p class="mt-0.5 text-xs text-muted-foreground">{description}</p>
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
				{@render children()}
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

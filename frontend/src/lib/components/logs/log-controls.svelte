<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import SwitchWithLabel from '#lib/components/form/labeled-switch.svelte';
	import { EllipsisIcon } from '#lib/icons/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';

	let {
		autoScroll = $bindable(),
		preferences,
		searchTerm = $bindable(''),
		mobileLayout = 'full',
		showDesktop = true,
		isStreaming = false,
		disabled = false,
		onStart,
		onStop,
		onRefresh
	}: {
		autoScroll: boolean;
		preferences: UseLogPreferences;
		searchTerm?: string;
		mobileLayout?: 'full' | 'menu-only' | 'actions-only' | 'none';
		showDesktop?: boolean;
		isStreaming?: boolean;
		disabled?: boolean;
		onStart?: () => void;
		onStop?: () => void;
		onRefresh?: () => void;
	} = $props();

	const tailOptions = [
		{ value: '50', label: m.log_tail_50_lines() },
		{ value: '100', label: m.log_tail_100_lines() },
		{ value: '200', label: m.log_tail_200_lines() },
		{ value: '500', label: m.log_tail_500_lines() },
		{ value: '1000', label: m.log_tail_1000_lines() },
		{ value: 'all', label: m.log_tail_all_lines() }
	];

	const parsedModeLabel = $derived.by(() => {
		if (preferences.showParsedJson) return m.common_parsed();
		return m.common_raw();
	});

	const selectedLabel = $derived(
		tailOptions.find((o) => o.value === preferences.selectedTail.current)?.label ?? m.log_tail_100_lines()
	);
</script>

{#snippet mobileActionButtons()}
	{#if isStreaming}
		<ArcaneButton
			action="stop"
			tone="ghost"
			size="icon"
			class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
			onclick={onStop}
			aria-label={m.common_stop()}
		/>
	{:else}
		<ArcaneButton
			action="start"
			tone="ghost"
			size="icon"
			class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
			onclick={onStart}
			aria-label={m.common_start()}
			{disabled}
		/>
	{/if}
	<ArcaneButton
		action="refresh"
		tone="ghost"
		size="icon"
		class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
		onclick={onRefresh}
		aria-label={m.log_refresh_aria_label()}
	/>
{/snippet}

{#snippet mobileMenu()}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
					aria-label={m.common_open_menu()}
				>
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>

		<DropdownMenu.Content align="end" class="w-72">
			<DropdownMenu.Label>{selectedLabel}</DropdownMenu.Label>
			<DropdownMenu.RadioGroup
				value={preferences.selectedTail.current}
				onValueChange={(value) => (preferences.selectedTail.current = value)}
			>
				{#each tailOptions as option (option.value)}
					<DropdownMenu.RadioItem value={option.value} disabled={isStreaming}>{option.label}</DropdownMenu.RadioItem>
				{/each}
			</DropdownMenu.RadioGroup>

			<DropdownMenu.Separator />

			<DropdownMenu.CheckboxItem
				checked={autoScroll}
				onCheckedChange={(checked) => {
					autoScroll = checked === true;
				}}
			>
				<div class="flex flex-col gap-0.5">
					<span class="font-medium">{m.common_autoscroll()}</span>
					<span class="text-xs text-muted-foreground">{m.log_auto_scroll_tooltip()}</span>
				</div>
			</DropdownMenu.CheckboxItem>
			<DropdownMenu.CheckboxItem
				checked={preferences.autoStartLogs}
				onCheckedChange={(checked) => {
					preferences.autoStartLogs = checked === true;
				}}
			>
				<div class="flex flex-col gap-0.5">
					<span class="font-medium">{m.auto_start()}</span>
					<span class="text-xs text-muted-foreground">{m.log_auto_start_tooltip()}</span>
				</div>
			</DropdownMenu.CheckboxItem>
			<DropdownMenu.CheckboxItem
				checked={preferences.showParsedJson}
				onCheckedChange={(checked) => {
					preferences.setParsedMode(checked === true);
				}}
			>
				<div class="flex flex-col gap-0.5">
					<span class="font-medium">{parsedModeLabel}</span>
					<span class="text-xs text-muted-foreground">{m.log_parsed_mode_tooltip()}</span>
				</div>
			</DropdownMenu.CheckboxItem>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

{#if mobileLayout !== 'none'}
	<div class="lg:hidden">
		{#if mobileLayout === 'full'}
			<div class="flex items-center justify-end gap-1">
				{@render mobileActionButtons()}

				{@render mobileMenu()}
			</div>
		{:else if mobileLayout === 'actions-only'}
			<div class="flex items-center justify-end gap-1">
				{@render mobileActionButtons()}
			</div>
		{:else if mobileLayout === 'menu-only'}
			<div class="flex items-center justify-end">
				{@render mobileMenu()}
			</div>
		{/if}
	</div>
{/if}

{#if showDesktop}
	<div class="hidden flex-col gap-3 lg:flex lg:flex-row lg:items-center lg:justify-end">
		<div class="flex flex-wrap items-center gap-4">
			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						<SwitchWithLabel
							triggerProps={props}
							id="auto-scroll-toggle"
							checked={autoScroll}
							label={m.common_autoscroll()}
							onCheckedChange={(checked) => {
								autoScroll = checked;
							}}
						/>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content side="bottom" class="max-w-xs">
					{m.log_auto_scroll_tooltip()}
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>

			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						<SwitchWithLabel
							triggerProps={props}
							id="auto-start-logs-toggle"
							checked={preferences.autoStartLogs}
							label={m.auto_start()}
							onCheckedChange={(checked) => {
								preferences.autoStartLogs = checked;
							}}
						/>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content side="bottom" class="max-w-xs">
					{m.log_auto_start_tooltip()}
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>

			<ArcaneTooltip.Root>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						<SwitchWithLabel
							triggerProps={props}
							id="parsed-log-mode-toggle"
							checked={preferences.showParsedJson}
							label={parsedModeLabel}
							onCheckedChange={(checked) => {
								preferences.setParsedMode(checked);
							}}
						/>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content side="bottom" class="max-w-xs">
					{m.log_parsed_mode_tooltip()}
				</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		</div>

		<Input type="search" placeholder={m.common_search()} bind:value={searchTerm} class="h-9 w-44 text-xs" />

		<Select.Root
			type="single"
			value={preferences.selectedTail.current}
			disabled={isStreaming}
			onValueChange={(v: string) => (preferences.selectedTail.current = v)}
		>
			<Select.Trigger class="h-9 w-32 text-xs">
				<span>{selectedLabel}</span>
			</Select.Trigger>
			<Select.Content>
				{#each tailOptions as option (option.value)}
					<Select.Item value={option.value}>{option.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>

		<div class="flex items-center gap-3">
			{#if isStreaming}
				<ArcaneButton action="stop" tone="outline" size="sm" class="text-xs font-medium" onclick={onStop} />
			{:else}
				<ArcaneButton action="start" tone="outline" size="sm" class="text-xs font-medium" onclick={onStart} {disabled} />
			{/if}
			<ArcaneButton
				action="refresh"
				tone="outline"
				size="sm"
				class="text-xs font-medium"
				onclick={onRefresh}
				aria-label={m.log_refresh_aria_label()}
				title={m.common_refresh()}
			/>
		</div>
	</div>
{/if}

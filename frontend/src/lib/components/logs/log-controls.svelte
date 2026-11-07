<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import type { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import { DownloadIcon, EyeOnIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let {
		autoScroll = $bindable(),
		preferences,
		searchTerm = $bindable(''),
		serviceLabels = false,
		isStreaming = false,
		disabled = false,
		onStart,
		onStop,
		onRefresh,
		onDownload
	}: {
		autoScroll: boolean;
		preferences: UseLogPreferences;
		searchTerm?: string;
		serviceLabels?: boolean;
		isStreaming?: boolean;
		disabled?: boolean;
		onStart?: () => void;
		onStop?: () => void;
		onRefresh?: () => void;
		onDownload?: () => void;
	} = $props();

	const tailOptions = [
		{ value: '50', label: m.log_tail_50_lines() },
		{ value: '100', label: m.log_tail_100_lines() },
		{ value: '200', label: m.log_tail_200_lines() },
		{ value: '500', label: m.log_tail_500_lines() },
		{ value: '1000', label: m.log_tail_1000_lines() },
		{ value: 'all', label: m.log_tail_all_lines() }
	];

	const selectedLabel = $derived(
		tailOptions.find((o) => o.value === preferences.selectedTail.current)?.label ?? m.log_tail_100_lines()
	);
</script>

{#snippet displayOption(label: string, description: string, checked: boolean, onChange: (checked: boolean) => void)}
	<DropdownMenu.CheckboxItem {checked} onCheckedChange={(value) => onChange(value === true)}>
		<div class="flex flex-col gap-0.5">
			<span class="font-medium">{label}</span>
			<span class="text-xs text-muted-foreground">{description}</span>
		</div>
	</DropdownMenu.CheckboxItem>
{/snippet}

<div class="flex min-w-0 items-center justify-end gap-1">
	<Input
		type="search"
		placeholder={m.common_search()}
		bind:value={searchTerm}
		size="sm"
		class="hidden h-8 w-44 sm:block"
		aria-label={m.common_search()}
	/>

	<Select.Root
		type="single"
		value={preferences.selectedTail.current}
		disabled={isStreaming}
		onValueChange={(v: string) => (preferences.selectedTail.current = v)}
	>
		<Select.Trigger class="hidden h-8 w-32 sm:flex">
			<span>{selectedLabel}</span>
		</Select.Trigger>
		<Select.Content>
			{#each tailOptions as option (option.value)}
				<Select.Item value={option.value}>{option.label}</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>

	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					icon={EyeOnIcon}
					customLabel={m.display()}
					class="h-8 shrink-0 text-muted-foreground hover:text-foreground"
				/>
			{/snippet}
		</DropdownMenu.Trigger>

		<DropdownMenu.Content align="end" class="w-72">
			<div class="sm:hidden">
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
			</div>

			{@render displayOption(m.common_autoscroll(), m.log_auto_scroll_tooltip(), autoScroll, (checked) => {
				autoScroll = checked;
			})}
			{@render displayOption(m.log_timestamps(), m.log_timestamps_tooltip(), preferences.showTimestamps, (checked) => {
				preferences.showTimestamps = checked;
			})}
			{#if serviceLabels}
				{@render displayOption(m.log_stream_labels(), m.log_stream_labels_tooltip(), preferences.showStreamLabels, (checked) => {
					preferences.showStreamLabels = checked;
				})}
			{/if}
			{@render displayOption(m.log_raw_output(), m.log_raw_output_tooltip(), !preferences.showParsedJson, (checked) => {
				preferences.setParsedMode(!checked);
			})}
			{@render displayOption(m.auto_start(), m.log_auto_start_tooltip(), preferences.autoStartLogs, (checked) => {
				preferences.autoStartLogs = checked;
			})}
		</DropdownMenu.Content>
	</DropdownMenu.Root>

	{#if isStreaming}
		<ArcaneButton
			action="stop"
			tone="ghost"
			size="icon"
			class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
			onclick={onStop}
			aria-label={m.common_stop()}
			title={m.common_stop()}
		/>
	{:else}
		<ArcaneButton
			action="start"
			tone="ghost"
			size="icon"
			class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
			onclick={onStart}
			aria-label={m.common_start()}
			title={m.common_start()}
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
		title={m.common_refresh()}
	/>
	{#if onDownload}
		<ArcaneButton
			action="base"
			tone="ghost"
			size="icon"
			icon={DownloadIcon}
			class="size-8 shrink-0 text-muted-foreground hover:text-foreground"
			onclick={onDownload}
			aria-label={m.common_download()}
			title={m.log_download_tooltip()}
		/>
	{/if}
</div>

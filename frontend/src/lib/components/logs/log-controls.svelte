<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import * as Select from '$lib/components/ui/select';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';

	let {
		autoScroll = $bindable(),
		tailLines = $bindable(100),
		autoStartLogs = $bindable(false),
		showParsedJson = $bindable(false),
		isStreaming = false,
		disabled = false,
		onStart,
		onStop,
		onRefresh
	}: {
		autoScroll: boolean;
		tailLines?: number;
		autoStartLogs?: boolean;
		showParsedJson?: boolean;
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

	const persistedTailLines = new PersistedState('arcane_log_tail_lines', '100');
	const persistedAutoStart = new PersistedState('arcane_log_auto_start', 'false');
	const persistedJsonParsing = new PersistedState('arcane_log_json_parsing_v3', 'false');

	let selectedTail = $state<string>(persistedTailLines.current || (tailLines >= 999999 ? 'all' : String(tailLines)));

	$effect(() => {
		persistedTailLines.current = selectedTail;
		if (selectedTail === 'all') {
			tailLines = 999999;
		} else {
			tailLines = parseInt(selectedTail, 10);
		}
	});

	$effect(() => {
		autoStartLogs = persistedAutoStart.current === 'true';
	});

	$effect(() => {
		persistedAutoStart.current = autoStartLogs ? 'true' : 'false';
	});

	$effect(() => {
		showParsedJson = persistedJsonParsing.current === 'true';
	});

	$effect(() => {
		persistedJsonParsing.current = showParsedJson ? 'true' : 'false';
	});

	const selectedLabel = $derived(tailOptions.find((o) => o.value === selectedTail)?.label ?? m.log_tail_100_lines());
</script>

<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-end">
	<div class="flex flex-wrap items-center gap-4">
		<ArcaneTooltip.Root>
			<ArcaneTooltip.Trigger>
				<SwitchWithLabel
					id="auto-scroll-toggle"
					checked={autoScroll}
					label={m.common_autoscroll()}
					onCheckedChange={(checked) => {
						autoScroll = checked;
					}}
				/>
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content side="bottom" class="max-w-xs">
				{m.log_auto_scroll_tooltip()}
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>

		<ArcaneTooltip.Root>
			<ArcaneTooltip.Trigger>
				<SwitchWithLabel
					id="auto-start-logs-toggle"
					checked={autoStartLogs}
					label={m.auto_start()}
					onCheckedChange={(checked) => {
						autoStartLogs = checked;
					}}
				/>
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content side="bottom" class="max-w-xs">
				{m.log_auto_start_tooltip()}
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>

		<ArcaneTooltip.Root>
			<ArcaneTooltip.Trigger>
				<SwitchWithLabel
					id="parsed-log-mode-toggle"
					checked={showParsedJson}
					label={showParsedJson ? m.common_parsed() : m.common_raw()}
					onCheckedChange={(checked) => {
						showParsedJson = checked;
					}}
				/>
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content side="bottom" class="max-w-xs">
				{m.log_parsed_mode_tooltip()}
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>
	</div>

	<Select.Root type="single" bind:value={selectedTail} disabled={isStreaming} onValueChange={(v: string) => (selectedTail = v)}>
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

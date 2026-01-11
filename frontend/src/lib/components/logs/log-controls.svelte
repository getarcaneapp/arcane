<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Select from '$lib/components/ui/select';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';
	import { RefreshIcon } from '$lib/icons';

	let {
		autoScroll = $bindable(),
		tailLines = $bindable(100),
		autoStartLogs = $bindable(false),
		isStreaming = false,
		disabled = false,
		onStart,
		onStop,
		onClear,
		onRefresh
	}: {
		autoScroll: boolean;
		tailLines?: number;
		autoStartLogs?: boolean;
		isStreaming?: boolean;
		disabled?: boolean;
		onStart?: () => void;
		onStop?: () => void;
		onClear?: () => void;
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

	let selectedTail = $state<string>(persistedTailLines.current);

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

	const selectedLabel = $derived(tailOptions.find((o) => o.value === selectedTail)?.label ?? m.log_tail_100_lines());
</script>

<div class="flex flex-col gap-3 sm:flex-row sm:items-center">
	<label class="flex items-center gap-2">
		<input type="checkbox" bind:checked={autoScroll} class="size-4" />
		<span class="text-sm font-medium">{m.common_autoscroll()}</span>
	</label>

	<label class="flex items-center gap-2">
		<input type="checkbox" bind:checked={autoStartLogs} class="size-4" />
		<span class="text-sm font-medium">{m.auto_start()}</span>
	</label>

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

	<div class="flex items-center gap-2">
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			class="text-xs font-medium"
			onclick={onClear}
			customLabel={m.common_clear()}
		/>
		{#if isStreaming}
			<ArcaneButton action="stop" tone="outline" size="sm" class="text-xs font-medium" onclick={onStop} />
		{:else}
			<ArcaneButton action="start" tone="outline" size="sm" class="text-xs font-medium" onclick={onStart} {disabled} />
		{/if}
		<ArcaneButton
			action="refresh"
			tone="outline"
			size="sm"
			class="px-2"
			onclick={onRefresh}
			aria-label={m.log_refresh_aria_label()}
			title={m.common_refresh()}
			showLabel={false}
		/>
	</div>
</div>

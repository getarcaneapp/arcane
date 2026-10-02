<script lang="ts">
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { useId } from 'bits-ui';

	export type PruneModeOption = {
		value: string;
		label: string;
		destructive?: boolean;
	};

	type DurationUnit = 'minutes' | 'hours' | 'days';

	// Segmented prune-mode choice with an optional "older than" duration; the host row supplies the label.
	interface Props {
		modeOptions: PruneModeOption[];
		value?: string;
		untilValue?: string;
		disabled?: boolean;
		olderThanMode?: string;
		warningTitle?: string;
		warningDescription?: string;
	}

	let {
		modeOptions,
		value = $bindable<string>(),
		untilValue = $bindable(''),
		disabled = false,
		olderThanMode = 'olderThan',
		warningTitle,
		warningDescription
	}: Props = $props();

	const defaultDurationAmountInternal = '24';
	const defaultDurationUnitInternal: DurationUnit = 'hours';
	const durationUnitOptionsInternal: { value: DurationUnit; label: string }[] = [
		{ value: 'minutes', label: m.prune_duration_unit_minutes() },
		{ value: 'hours', label: m.prune_duration_unit_hours() },
		{ value: 'days', label: m.prune_duration_unit_days() }
	];

	const durationUnitId = `prune-duration-unit-${useId()}`;
	const hasOlderThan = $derived(modeOptions.some((option) => option.value === olderThanMode));
	const selectedOption = $derived(modeOptions.find((option) => option.value === value));
	const parsedDuration = $derived(parsePruneDurationInternal(untilValue));

	function handleModeChange(nextMode: string) {
		if (disabled) return;
		value = nextMode;
		if (nextMode === olderThanMode && !untilValue) {
			untilValue = serializePruneDurationInternal(defaultDurationAmountInternal, defaultDurationUnitInternal);
		}
	}

	function handleDurationAmountInputInternal(event: Event) {
		untilValue = serializePruneDurationInternal((event.currentTarget as HTMLInputElement).value, parsedDuration.unit);
	}

	function handleDurationUnitChangeInternal(nextUnit: string) {
		untilValue = serializePruneDurationInternal(parsedDuration.amount, nextUnit as DurationUnit);
	}

	function parsePruneDurationInternal(valueToParse: string): { amount: string; unit: DurationUnit } {
		const trimmed = valueToParse.trim();
		if (!trimmed) {
			return { amount: defaultDurationAmountInternal, unit: defaultDurationUnitInternal };
		}
		if (trimmed.endsWith('m')) {
			return { amount: trimmed.slice(0, -1) || defaultDurationAmountInternal, unit: 'minutes' };
		}
		if (trimmed.endsWith('h')) {
			const rawHours = Number(trimmed.slice(0, -1));
			if (Number.isFinite(rawHours) && rawHours > 0 && rawHours % 24 === 0) {
				return { amount: String(rawHours / 24), unit: 'days' };
			}
			return { amount: trimmed.slice(0, -1) || defaultDurationAmountInternal, unit: 'hours' };
		}
		return { amount: defaultDurationAmountInternal, unit: defaultDurationUnitInternal };
	}

	function serializePruneDurationInternal(amount: string, unit: DurationUnit): string {
		const parsedAmount = Math.max(1, Number(amount) || 1);
		switch (unit) {
			case 'minutes':
				return `${parsedAmount}m`;
			case 'days':
				return `${parsedAmount * 24}h`;
			default:
				return `${parsedAmount}h`;
		}
	}
</script>

<div class="flex flex-col gap-2.5">
	<Tabs.Root {value} onValueChange={handleModeChange}>
		<Tabs.List class="min-h-8 w-full sm:w-fit">
			{#each modeOptions as option (option.value)}
				<Tabs.Trigger
					value={option.value}
					{disabled}
					size="sm"
					variant={option.destructive ? 'destructive' : 'default'}
					class="h-7 flex-1 sm:flex-none"
				>
					{option.label}
				</Tabs.Trigger>
			{/each}
		</Tabs.List>
	</Tabs.Root>

	{#if hasOlderThan && value === olderThanMode}
		<div class="flex flex-wrap items-center gap-2">
			<Input
				type="number"
				min="1"
				value={parsedDuration.amount}
				oninput={handleDurationAmountInputInternal}
				{disabled}
				size="sm"
				class="h-8 w-24"
				placeholder={m.prune_duration_placeholder()}
				aria-label={m.prune_duration_help()}
			/>
			<SelectWithLabel
				id={durationUnitId}
				label={m.prune_duration_help()}
				hideLabel
				compact
				triggerSize="sm"
				value={parsedDuration.unit}
				options={durationUnitOptionsInternal}
				onValueChange={handleDurationUnitChangeInternal}
				{disabled}
			/>
		</div>
		<p class="text-xs text-muted-foreground">{m.prune_duration_help()}</p>
	{/if}

	{#if warningDescription && selectedOption?.destructive}
		<div class="rounded-md border border-warning/30 bg-warning/10 p-2 text-xs text-warning">
			{#if warningTitle}
				<p class="font-medium">{warningTitle}</p>
			{/if}
			<p>{warningDescription}</p>
		</div>
	{/if}
</div>

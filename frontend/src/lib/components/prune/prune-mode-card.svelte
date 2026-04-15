<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages';

	export type PruneModeOption = {
		value: string;
		label: string;
		destructive?: boolean;
	};

	type DurationUnit = 'minutes' | 'hours' | 'days';

	interface Props {
		title: string;
		description: string;
		modeOptions: PruneModeOption[];
		value?: string;
		untilValue?: string;
		disabled?: boolean;
		olderThanMode?: string;
		warningTitle?: string;
		warningDescription?: string;
	}

	let {
		title,
		description,
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

	function hasOlderThanInternal(): boolean {
		return modeOptions.some((option) => option.value === olderThanMode);
	}

	function getSelectedOptionInternal(): PruneModeOption | undefined {
		return modeOptions.find((option) => option.value === value);
	}

	let parsedDurationInternal = $derived.by(() => parsePruneDurationInternal(untilValue));
	let durationAmountInternal = $derived(parsedDurationInternal.amount);
	let durationUnitInternal = $derived(parsedDurationInternal.unit);

	function handleModeChange(nextMode: string) {
		if (disabled) return;
		value = nextMode;
		if (nextMode === olderThanMode && !untilValue) {
			untilValue = serializePruneDurationInternal(defaultDurationAmountInternal, defaultDurationUnitInternal);
		}
	}

	function updateDurationAmountInternal(nextAmount: string) {
		untilValue = serializePruneDurationInternal(nextAmount, durationUnitInternal);
	}

	function updateDurationUnitInternal(nextUnit: DurationUnit) {
		untilValue = serializePruneDurationInternal(durationAmountInternal, nextUnit);
	}

	function handleDurationAmountInputInternal(event: Event) {
		updateDurationAmountInternal((event.currentTarget as HTMLInputElement).value);
	}

	function handleDurationUnitChangeInternal(event: Event) {
		updateDurationUnitInternal((event.currentTarget as HTMLSelectElement).value as DurationUnit);
	}

	function parsePruneDurationInternal(valueToParse: string): { amount: string; unit: DurationUnit } {
		const trimmed = valueToParse.trim();
		if (!trimmed) {
			return {
				amount: defaultDurationAmountInternal,
				unit: defaultDurationUnitInternal
			};
		}

		if (trimmed.endsWith('m')) {
			return {
				amount: trimmed.slice(0, -1) || defaultDurationAmountInternal,
				unit: 'minutes'
			};
		}

		if (trimmed.endsWith('h')) {
			const rawHours = Number(trimmed.slice(0, -1));
			if (Number.isFinite(rawHours) && rawHours > 0 && rawHours % 24 === 0) {
				return {
					amount: String(rawHours / 24),
					unit: 'days'
				};
			}

			return {
				amount: trimmed.slice(0, -1) || defaultDurationAmountInternal,
				unit: 'hours'
			};
		}

		return {
			amount: defaultDurationAmountInternal,
			unit: defaultDurationUnitInternal
		};
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

<div class="bg-muted/20 ring-border/20 flex h-full flex-col gap-2.5 rounded-lg p-3 ring-1">
	<div class="space-y-0.5">
		<p class="text-xs font-medium">{title}</p>
		<p class="text-muted-foreground text-[11px] leading-tight">{description}</p>
	</div>

	<Tabs.Root {value} onValueChange={handleModeChange}>
		<Tabs.List class="h-8 w-full">
			{#each modeOptions as option (option.value)}
				<Tabs.Trigger
					value={option.value}
					{disabled}
					class={cn(
						'h-6 flex-1 text-xs',
						option.destructive && value === option.value && 'bg-destructive/15 text-destructive hover:bg-destructive/10 shadow-sm'
					)}
				>
					{option.label}
				</Tabs.Trigger>
			{/each}
		</Tabs.List>
	</Tabs.Root>

	{#if hasOlderThanInternal() && value === olderThanMode}
		<div class="grid gap-1.5 sm:grid-cols-[minmax(0,1fr)_auto]">
			<Input
				type="number"
				min="1"
				value={durationAmountInternal}
				oninput={handleDurationAmountInputInternal}
				{disabled}
				class="h-8 text-xs"
				placeholder={m.prune_duration_placeholder()}
			/>
			<select
				class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring h-8 rounded-md border px-2.5 py-1 text-xs focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
				value={durationUnitInternal}
				onchange={handleDurationUnitChangeInternal}
				{disabled}
			>
				{#each durationUnitOptionsInternal as option (option.value)}
					<option value={option.value}>{option.label}</option>
				{/each}
			</select>
		</div>
		<p class="text-muted-foreground text-xs">{m.prune_duration_help()}</p>
	{/if}

	{#if warningDescription && getSelectedOptionInternal()?.destructive}
		<div class="mt-auto rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-xs text-amber-900 dark:text-amber-200">
			{#if warningTitle}
				<p class="font-medium">{warningTitle}</p>
			{/if}
			<p>{warningDescription}</p>
		</div>
	{/if}
</div>

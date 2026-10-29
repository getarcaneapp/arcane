<script lang="ts">
	import { mode } from 'mode-watcher';

	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { CheckIcon, AddIcon } from '#lib/icons/index.js';
	import { resolveThemePrimary } from '#lib/utils/theme.svelte.js';

	import CustomColorDialog from './custom-color.svelte';

	type Swatch = { label: string; color: string };

	const defaultColors: Swatch[] = [
		{ label: 'Purple', color: 'oklch(0.606 0.25 292.717)' },
		{ label: 'Rose', color: 'oklch(0.63 0.2 15)' },
		{ label: 'Orange', color: 'oklch(0.68 0.2 50)' },
		{ label: 'Amber', color: 'oklch(0.75 0.18 80)' },
		{ label: 'Green', color: 'oklch(0.65 0.2 150)' },
		{ label: 'Teal', color: 'oklch(0.6 0.15 180)' },
		{ label: 'Blue', color: 'oklch(0.6 0.2 240)' }
	];

	let {
		selectedColor = $bindable(),
		previousColor,
		disabled = false,
		colors = defaultColors,
		withTheme = true,
		disabledColors = [],
		onSelect,
		onCustomColor
	}: {
		selectedColor: string;
		previousColor: string;
		disabled?: boolean;
		colors?: Swatch[];
		withTheme?: boolean;
		disabledColors?: string[];
		onSelect?: (color: string) => void;
		onCustomColor?: () => void;
	} = $props();
	let showCustomColorDialog = $state(false);

	const uid = $props.id();
	const themePrimary = $derived(resolveThemePrimary(mode.current === 'dark'));

	let isCustomColor = $derived(selectedColor !== 'theme' && !colors.some((c) => c.color === selectedColor));
	let isPreviousColorCustom = $derived(previousColor !== 'theme' && !colors.some((c) => c.color === previousColor));

	function handleAccentColorChange(accentValue: string) {
		const storedValue = accentValue === 'theme' ? 'default' : accentValue;
		selectedColor = accentValue;
		onSelect?.(storedValue);
	}
</script>

<RadioGroup.Root
	class="flex flex-wrap"
	value={isCustomColor ? 'custom' : selectedColor}
	onValueChange={(value) => {
		if (value != 'custom') {
			handleAccentColorChange(value);
		}
	}}
>
	{#if withTheme}
		{@render colorOption('Theme', 'theme', selectedColor === 'theme')}
	{/if}
	{#each colors as accent (accent.color)}
		{@render colorOption(accent.label, accent.color, selectedColor === accent.color)}
	{/each}
	{#if isCustomColor || isPreviousColorCustom}
		{@render colorOption('Custom', isCustomColor ? selectedColor : previousColor, isCustomColor)}
	{/if}
	{@render colorOption('Custom', 'custom', false, true)}
</RadioGroup.Root>

<CustomColorDialog bind:open={showCustomColorDialog} onApply={handleAccentColorChange} />

{#snippet colorOption(label: string, color: string, isSelected: boolean, isCustomColorSelection = false)}
	{@const isDisabled = disabled || disabledColors.includes(color)}
	<div class="group/item relative">
		<RadioGroup.Item id={`${uid}-${color}`} value={color} class="sr-only" disabled={isDisabled} />
		<Label
			for={`${uid}-${color}`}
			class={{
				'cursor-pointer': !isDisabled,
				'cursor-not-allowed': isDisabled,
				group: isCustomColorSelection
			}}
			onclick={() => {
				if (!isCustomColorSelection || disabled) return;
				if (onCustomColor) onCustomColor();
				else showCustomColorDialog = true;
			}}
		>
			<ArcaneTooltip.Root delayDuration={200}>
				<ArcaneTooltip.Trigger>
					{#snippet child({ props })}
						<div
							{...props}
							tabindex="-1"
							class={{
								'relative z-(--arcane-z-raised) size-8 rounded-full border-2 bg-(--swatch) transition-all duration-200 ease-out': true,
								'group-hover/item:z-(--arcane-z-sticky) group-hover/item:scale-110': !isDisabled,
								'opacity-30': isDisabled
							}}
							style={`--swatch: ${color === 'theme' ? themePrimary : color}`}
						>
							{#if isCustomColorSelection}
								<div
									class="absolute inset-0 flex items-center justify-center rounded-full border-2 border-dashed border-border bg-muted"
								>
									<AddIcon class="size-4 text-muted-foreground" />
								</div>
							{:else if isSelected}
								<div class="absolute inset-0 flex items-center justify-center">
									<CheckIcon class="size-6 text-white drop-shadow-sm" />
								</div>
							{/if}
						</div>
					{/snippet}
				</ArcaneTooltip.Trigger>
				<ArcaneTooltip.Content>{label}</ArcaneTooltip.Content>
			</ArcaneTooltip.Root>
		</Label>
	</div>
{/snippet}

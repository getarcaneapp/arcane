<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ArrowsUpDownIcon, FileTextIcon, SearchIcon } from '#lib/icons/index.js';

	interface Props {
		outlineOpen: boolean;
		outlineLabel: string;
		onToggleOutline: () => void;
		showDiff?: boolean;
		diffOpen?: boolean;
		diffLabel?: string;
		onToggleDiff?: () => void;
		commandPaletteLabel: string;
		onOpenCommandPalette: () => void;
	}

	let {
		outlineOpen,
		outlineLabel,
		onToggleOutline,
		showDiff = true,
		diffOpen = false,
		diffLabel,
		onToggleDiff,
		commandPaletteLabel,
		onOpenCommandPalette
	}: Props = $props();
</script>

<ArcaneButton
	action="base"
	tone={outlineOpen ? 'outline-primary' : 'ghost'}
	size="icon"
	class="size-6"
	showLabel={false}
	icon={FileTextIcon}
	customLabel={outlineLabel}
	onclick={onToggleOutline}
/>
{#if showDiff}
	<ArcaneButton
		action="base"
		tone={diffOpen ? 'outline-primary' : 'ghost'}
		size="icon"
		class="size-6"
		showLabel={false}
		icon={ArrowsUpDownIcon}
		customLabel={diffLabel}
		onclick={onToggleDiff}
	/>
{/if}
<ArcaneButton
	action="base"
	tone="ghost"
	size="icon"
	class="size-6"
	showLabel={false}
	icon={SearchIcon}
	customLabel={commandPaletteLabel}
	onclick={onOpenCommandPalette}
/>

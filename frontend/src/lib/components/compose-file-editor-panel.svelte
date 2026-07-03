<!--
	compose-file-editor-panel — the row of editor toolbar buttons rendered inside
	the tab strip of the tree-layout compose editor (outline toggle, optional diff
	toggle, command-palette open). Shared by the new-project, edit-project, and
	new-swarm-stack compose pages.

	The buttons are the only markup that was duplicated verbatim across all three
	pages; the surrounding ResizableSplit + file tree + CodePanel wiring is kept in
	each page because its bindings (compose/env value + validation state, per-file
	content, read-only + diff + load-error handling, persistKey, resize callbacks)
	differ per page and cannot be unified without changing behavior.
-->
<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ArrowsUpDownIcon, FileTextIcon, SearchIcon } from '$lib/icons';

	interface Props {
		outlineOpen: boolean;
		outlineLabel: string;
		onToggleOutline: () => void;
		// When showDiff is false the diff toggle is omitted (new-swarm-stack page).
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

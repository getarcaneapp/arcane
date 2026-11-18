<script lang="ts">
	import { Checkbox } from '@arcane/ui/checkbox';
	import type { ComponentProps } from 'svelte';

	import type { SelectionModifiers } from './arcane-table.types.svelte';

	let {
		checked = false,
		onCheckedChange,
		onclick,
		onkeydown,
		...restProps
	}: Omit<ComponentProps<typeof Checkbox>, 'onCheckedChange'> & {
		onCheckedChange?: (checked: boolean, modifiers: SelectionModifiers) => void;
	} = $props();

	let shiftKey = false;

	function handleCheckedChange(value: boolean) {
		const modifiers = { shiftKey };
		shiftKey = false;
		onCheckedChange?.(value, modifiers);
	}
</script>

<Checkbox
	{checked}
	onCheckedChange={handleCheckedChange}
	onclick={(event) => {
		shiftKey = event.shiftKey;
		onclick?.(event);
	}}
	onkeydown={(event) => {
		shiftKey = event.key === ' ' && event.shiftKey;
		onkeydown?.(event);
	}}
	{...restProps}
/>

<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	import * as InputGroup from '#lib/components/input-group/index.js';
	import { CloseIcon, SearchIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	let {
		value = $bindable(''),
		ref = $bindable(null),
		onClear,
		placeholder = m.common_search(),
		...props
	}: Omit<HTMLInputAttributes, 'value' | 'size'> & {
		value?: string;
		ref?: HTMLInputElement | null;
		onClear: () => void;
	} = $props();
</script>

<InputGroup.Root>
	<InputGroup.Addon><SearchIcon aria-hidden="true" /></InputGroup.Addon>
	<InputGroup.Input type="text" {placeholder} bind:value bind:ref {...props} />
	{#if value}
		<InputGroup.Addon align="inline-end">
			<InputGroup.Button size="icon-xs" onclick={onClear} title={m.common_clear_search()} aria-label={m.common_clear_search()}>
				<CloseIcon class="size-4" />
			</InputGroup.Button>
		</InputGroup.Addon>
	{/if}
</InputGroup.Root>

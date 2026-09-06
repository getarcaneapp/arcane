<script lang="ts">
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Props {
		onclick: () => void;
		disabled?: boolean;
		withSeparator?: boolean;
		label?: string;
	}

	let { onclick, disabled = false, withSeparator = true, label = m.common_remove() }: Props = $props();
</script>

{#if withSeparator}
	<DropdownMenu.Separator />
{/if}
<DropdownMenu.Item variant="destructive" {onclick} {disabled}>
	<TrashIcon class="size-4" />
	{label}
</DropdownMenu.Item>

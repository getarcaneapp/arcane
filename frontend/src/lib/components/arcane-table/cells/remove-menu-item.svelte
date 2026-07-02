<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		onclick: () => void;
		disabled?: boolean;
		withSeparator?: boolean;
	}

	let { onclick, disabled = false, withSeparator = true }: Props = $props();
</script>

{#if withSeparator}
	<DropdownMenu.Separator />
{/if}
<DropdownMenu.Item variant="destructive" {onclick} {disabled}>
	<TrashIcon class="size-4" />
	{m.common_remove()}
</DropdownMenu.Item>

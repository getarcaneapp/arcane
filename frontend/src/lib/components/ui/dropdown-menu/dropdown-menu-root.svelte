<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
	import { setContext } from 'svelte';
	import { dropdownMenuContextKey } from './dropdown-menu-context';

	let { open = $bindable(false), ...restProps }: DropdownMenuPrimitive.RootProps = $props();

	setContext(dropdownMenuContextKey, {
		close: () => {
			open = false;
		}
	});
</script>

<DropdownMenuPrimitive.Root bind:open {...restProps} />

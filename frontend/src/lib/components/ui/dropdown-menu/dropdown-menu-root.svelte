<script lang="ts">
	import { DropdownMenu as DropdownMenuPrimitive } from 'bits-ui';
	import { setDropdownMenuContext } from './dropdown-menu-context';

	let { open = $bindable(false), ...restProps }: DropdownMenuPrimitive.RootProps = $props();

	setDropdownMenuContext({
		close: () => {
			open = false;
		}
	});
</script>

<DropdownMenuPrimitive.Root bind:open {...restProps} />

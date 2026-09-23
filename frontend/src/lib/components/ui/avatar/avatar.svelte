<script lang="ts">
	import { Avatar as AvatarPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		size = 'default',
		...restProps
	}: AvatarPrimitive.RootProps & { size?: 'default' | 'lg' } = $props();
</script>

<AvatarPrimitive.Root
	bind:ref
	data-slot="avatar"
	data-size={size}
	class={cn('relative flex shrink-0 overflow-hidden rounded-lg', size === 'lg' ? 'size-16 rounded-xl' : 'size-8', className)}
	{...restProps}
/>

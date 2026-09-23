<script lang="ts">
	import { Calendar as CalendarPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: CalendarPrimitive.HeadCellProps = $props();
</script>

<CalendarPrimitive.HeadCell
	bind:ref
	class={cn('w-(--cell-size) rounded-md text-xs font-normal text-muted-foreground', className)}
	{...restProps}
/>

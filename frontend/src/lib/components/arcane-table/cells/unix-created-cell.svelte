<script lang="ts">
	import { format } from 'date-fns';

	let { value }: { value: unknown } = $props();
</script>

{format(new Date(Number(value || 0) * 1000), 'PP p')}

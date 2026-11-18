<script lang="ts">
	import { Button } from '@arcane/ui/button';
	import type { Snippet } from 'svelte';

	interface Props {
		cancelLabel: string;
		isLoading?: boolean;
		onCancel: () => void;
		primary: Snippet;
	}

	let { cancelLabel, isLoading = false, onCancel, primary }: Props = $props();
</script>

<Button type="button" class="flex-1" variant="outline" onclick={onCancel} disabled={isLoading}>
	{cancelLabel}
</Button>

{@render primary()}

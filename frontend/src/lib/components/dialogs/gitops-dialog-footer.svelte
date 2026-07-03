<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Button } from '$lib/components/ui/button/index.js';

	interface Props {
		cancelLabel: string;
		isLoading?: boolean;
		onCancel: () => void;
		primary: Snippet;
	}

	let { cancelLabel, isLoading = false, onCancel, primary }: Props = $props();
</script>

<Button type="button" class="arcane-button-cancel flex-1" variant="outline" onclick={onCancel} disabled={isLoading}>
	{cancelLabel}
</Button>

{@render primary()}

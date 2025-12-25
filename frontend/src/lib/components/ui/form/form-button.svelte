<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';

	interface Props {
		ref?: HTMLButtonElement | null;
		variant?: 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link';
		size?: 'default' | 'sm' | 'lg' | 'icon';
		disabled?: boolean;
		class?: string;
		onclick?: () => void;
		children?: import('svelte').Snippet;
	}

	let { ref = $bindable(null), variant, size, disabled, class: className, onclick, children }: Props = $props();
</script>

<ArcaneButton
	bind:ref
	action="base"
	type="submit"
	tone={variant === 'destructive' ? 'outline-destructive' : variant === 'ghost' ? 'ghost' : 'outline'}
	{size}
	{disabled}
	class={className}
	{onclick}
>
	{@render children?.()}
</ArcaneButton>

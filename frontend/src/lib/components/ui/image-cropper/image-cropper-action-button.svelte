<script lang="ts" module>
	import type { Action, ArcaneButtonSize, ArcaneButtonTone } from '$lib/components/arcane-button';

	export type ImageCropperActionButtonProps = {
		ref?: HTMLElement | null;
		action?: Action;
		size?: ArcaneButtonSize;
		tone?: ArcaneButtonTone;
		customLabel?: string;
		disabled?: boolean;
		class?: string;
		onclick?: (event: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) => void;
	};
</script>

<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button';

	let {
		ref = $bindable(null),
		action,
		size,
		tone,
		customLabel,
		disabled,
		class: className = undefined,
		onclick,
		onAction
	}: ImageCropperActionButtonProps & { action: Action; onAction: () => void } = $props();
</script>

<ArcaneButton
	bind:ref
	{action}
	{size}
	{tone}
	{customLabel}
	{disabled}
	class={className}
	onclick={(event: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) => {
		onclick?.(event);
		onAction();
	}}
/>

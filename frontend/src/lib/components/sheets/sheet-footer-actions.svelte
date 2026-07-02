<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { Action, ArcaneButtonTone } from '$lib/components/arcane-button/variants';

	interface Props {
		open?: boolean;
		onCancel?: () => void;
		cancelDisabled?: boolean;
		showSubmit?: boolean;
		submitAction?: Action;
		submitTone?: ArcaneButtonTone;
		submitLabel?: string;
		submitDisabled?: boolean;
		submitLoading?: boolean;
		submitForm?: string;
		onSubmit?: (e: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) => void;
	}

	let {
		open = $bindable(),
		onCancel,
		cancelDisabled = false,
		showSubmit = true,
		submitAction = 'create',
		submitTone,
		submitLabel,
		submitDisabled = false,
		submitLoading = false,
		submitForm,
		onSubmit
	}: Props = $props();

	function handleCancel() {
		if (onCancel) {
			onCancel();
		} else {
			open = false;
		}
	}
</script>

<div class="flex w-full flex-row gap-2">
	<ArcaneButton action="cancel" tone="outline" type="button" class="flex-1" onclick={handleCancel} disabled={cancelDisabled} />
	{#if showSubmit}
		<ArcaneButton
			action={submitAction}
			tone={submitTone}
			type="submit"
			form={submitForm}
			class="flex-1"
			disabled={submitDisabled}
			loading={submitLoading}
			onclick={onSubmit}
			customLabel={submitLabel}
		/>
	{/if}
</div>

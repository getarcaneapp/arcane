<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { ArcaneButtonSize, ArcaneButtonTone } from '$lib/components/arcane-button';
	import { CopyIcon, CheckIcon } from '$lib/icons';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import type { ClassValue } from 'svelte/elements';

	interface Props {
		/** The text copied to the clipboard when the button is pressed. */
		text: string;
		/** Accessible label / tooltip. Defaults to "Copy". */
		label?: string;
		/** Show a toast on a successful copy. Defaults to true. */
		toastOnCopy?: boolean;
		/** Custom success toast message. Defaults to "Copied to clipboard". */
		successMessage?: string;
		size?: ArcaneButtonSize;
		tone?: ArcaneButtonTone;
		disabled?: boolean;
		class?: ClassValue;
	}

	let {
		text,
		label = m.common_copy(),
		toastOnCopy = true,
		successMessage,
		size = 'icon',
		tone = 'ghost',
		disabled = false,
		class: className
	}: Props = $props();

	const clipboard = new UseClipboard();

	async function handleCopy() {
		const status = await clipboard.copy(text);
		if (status === 'success') {
			if (toastOnCopy) toast.success(successMessage ?? m.common_copied());
		} else if (typeof window !== 'undefined' && !window.isSecureContext) {
			toast.error(m.common_copy_https_required());
		} else {
			toast.error(m.common_copy_failed());
		}
	}
</script>

<ArcaneButton
	action="base"
	{tone}
	{size}
	{disabled}
	onclick={handleCopy}
	title={label}
	customLabel={label}
	showLabel={size !== 'icon'}
	icon={clipboard.copied ? CheckIcon : CopyIcon}
	class={className}
/>

<!--
	Installed from @ieedan/shadcn-svelte-extras
-->

<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';
	import { cn } from '$lib/utils';
	import { scale } from 'svelte/transition';
	import type { CopyButtonProps } from './types';
	import { CopyIcon, CloseIcon, CheckIcon } from '$lib/icons';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { onMount } from 'svelte';
	import { m } from '$lib/paraglide/messages';

	let {
		ref = $bindable(null),
		text,
		icon,
		animationDuration = 500,
		variant = 'ghost',
		size = 'icon',
		onCopy,
		class: className,
		tabindex = -1,
		children
	}: CopyButtonProps = $props();

	const clipboard = new UseClipboard();

	const resolvedSize = $derived(size === 'icon' && children ? 'default' : size);

	let isSecure = $state(true);

	onMount(() => {
		isSecure = window.isSecureContext || window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1';
	});
</script>

{#if isSecure}
	<ArcaneButton
		bind:ref
		action="base"
		tone={variant === 'ghost' ? 'ghost' : variant === 'outline' ? 'outline' : 'outline'}
		size={resolvedSize}
		{tabindex}
		class={cn('flex items-center gap-2', className)}
		type="button"
		name="copy"
		onclick={async () => {
			const status = await clipboard.copy(text);

			onCopy?.(status);
		}}
	>
		{#if clipboard.status === 'success'}
			<div in:scale={{ duration: animationDuration, start: 0.85 }}>
				<CheckIcon tabindex={-1} />
				<span class="sr-only">Copied</span>
			</div>
		{:else if clipboard.status === 'failure'}
			<div in:scale={{ duration: animationDuration, start: 0.85 }}>
				<CloseIcon tabindex={-1} />
				<span class="sr-only">Failed to copy</span>
			</div>
		{:else}
			<div in:scale={{ duration: animationDuration, start: 0.85 }}>
				{#if icon}
					{@render icon()}
				{:else}
					<CopyIcon tabindex={-1} />
				{/if}
				<span class="sr-only">Copy</span>
			</div>
		{/if}
		{@render children?.()}
	</ArcaneButton>
{:else}
	<Tooltip.Root>
		<Tooltip.Trigger>
			<ArcaneButton
				bind:ref
				action="base"
				tone={variant === 'ghost' ? 'ghost' : variant === 'outline' ? 'outline' : 'outline'}
				size={resolvedSize}
				{tabindex}
				class={cn('flex cursor-not-allowed items-center gap-2 opacity-50', className)}
				type="button"
				name="copy"
				disabled
			>
				<div in:scale={{ duration: animationDuration, start: 0.85 }}>
					{#if icon}
						{@render icon()}
					{:else}
						<CopyIcon tabindex={-1} />
					{/if}
					<span class="sr-only">Copy</span>
				</div>
				{@render children?.()}
			</ArcaneButton>
		</Tooltip.Trigger>
		<Tooltip.Content>
			<p>{m.common_copy_https_required()}</p>
		</Tooltip.Content>
	</Tooltip.Root>
{/if}

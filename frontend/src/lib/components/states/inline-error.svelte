<script lang="ts">
	import { AlertIcon, RefreshIcon } from '$lib/icons';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { ClassValue } from 'svelte/elements';

	interface Props {
		message: string;
		title?: string;
		/** When provided, renders a retry button. */
		onRetry?: () => void;
		retryLabel?: string;
		class?: ClassValue;
	}

	let { message, title, onRetry, retryLabel = m.common_retry(), class: className }: Props = $props();
</script>

<div
	role="alert"
	class={cn('flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm', className)}
>
	<AlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
	<div class="min-w-0 flex-1 space-y-1">
		{#if title}
			<p class="font-medium">{title}</p>
		{/if}
		<p class="break-words text-muted-foreground">{message}</p>
	</div>
	{#if onRetry}
		<ArcaneButton
			action="base"
			tone="outline"
			size="sm"
			icon={RefreshIcon}
			customLabel={retryLabel}
			onclick={onRetry}
			class="shrink-0"
		/>
	{/if}
</div>

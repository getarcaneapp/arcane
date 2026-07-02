<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { m } from '$lib/paraglide/messages';

	interface Props {
		busy: boolean;
		busyTitle: string;
		busyDescription: string;
		error: string;
		children?: Snippet;
	}

	let { busy, busyTitle, busyDescription, error, children }: Props = $props();
</script>

<div class="bg-background flex min-h-screen items-center justify-center">
	<div class="w-full max-w-md space-y-8">
		<div class="flex flex-col items-center text-center">
			{#if busy}
				<Spinner class="text-primary size-12" />
				<h2 class="mt-6 text-2xl font-semibold">{busyTitle}</h2>
				<p class="text-muted-foreground mt-2 text-sm">{busyDescription}</p>
			{:else if error}
				<div class="text-destructive flex flex-col items-center">
					<svg class="h-12 w-12" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							stroke-width="2"
							d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.732-.833-2.5 0L3.341 16.5c-.77.833.192 2.5 1.732 2.5z"
						/>
					</svg>
					<h2 class="mt-6 text-2xl font-semibold">{m.auth_authentication_error_title()}</h2>
					<p class="mt-2 text-sm">{error}</p>
					<p class="text-muted-foreground mt-4 text-xs">{m.auth_redirecting_to_login()}</p>
				</div>
			{:else}
				{@render children?.()}
			{/if}
		</div>
	</div>
</div>

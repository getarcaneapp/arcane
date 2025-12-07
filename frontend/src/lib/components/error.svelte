<script lang="ts">
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { Button } from '$lib/components/ui/button';
	import CircleXIcon from 'phosphor-svelte/lib/XCircle';
	import { m } from '$lib/paraglide/messages';

	let {
		message,
		status,
		title = m.error_generic(),
		showButton = true,
		actionHref = '/dashboard',
		actionLabel = m.error_go_to_dashboard()
	}: {
		message: string;
		status?: number;
		title?: string;
		showButton?: boolean;
		actionHref?: string;
		actionLabel?: string;
	} = $props();
</script>

<div class="grid h-full min-h-screen place-items-center px-6">
	<Empty.Root>
		<Empty.Header>
			<Empty.Media variant="icon">
				<CircleXIcon class="text-destructive size-7" aria-hidden="true" />
			</Empty.Media>
			<Empty.Title>{title}</Empty.Title>
			<Empty.Description>{message} - {status}</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			{#if showButton}
				<Empty.Content>
					<Button href={actionHref}>{actionLabel}</Button>
				</Empty.Content>
			{/if}
		</Empty.Content>
	</Empty.Root>
</div>

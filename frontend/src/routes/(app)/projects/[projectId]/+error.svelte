<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { AlertIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	const status = $derived(page.status ?? 500);
	const message = $derived(page.error?.message ?? m.error_generic());
</script>

<div class="flex h-full flex-1 flex-col items-center justify-center p-8">
	<Empty.Root>
		<Empty.Header>
			<Empty.Media variant="icon">
				<AlertIcon class="text-destructive size-16" aria-hidden="true" />
			</Empty.Media>
			<Empty.Title>
				{status === 404 ? m.error_not_found() : 'Failed to load project'}
			</Empty.Title>
			<Empty.Description>
				{message}
			</Empty.Description>
		</Empty.Header>
		<Empty.Content>
			<div class="flex flex-col items-center gap-3">
				<ArcaneButton action="base" customLabel="Back to Projects" onclick={() => goto('/projects')} />
			</div>
		</Empty.Content>
	</Empty.Root>
</div>

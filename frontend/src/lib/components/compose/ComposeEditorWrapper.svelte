<script lang="ts">
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { AlertIcon, ExternalLinkIcon } from '$lib/icons';
	import * as m from '$lib/paraglide/messages';
	import { toast } from 'svelte-sonner';
	import { invalidateAll } from '$app/navigation';
	import type { Snippet } from 'svelte';

	let {
		projectId,
		projectName,
		gitOpsManagedBy = undefined,
		fileTitle,
		serviceName = undefined,
		isDirty,
		onSave,
		children
	}: {
		projectId: string;
		projectName: string;
		gitOpsManagedBy?: string;
		fileTitle: string;
		serviceName?: string;
		isDirty: boolean;
		onSave: () => Promise<void>;
		children: Snippet;
	} = $props();

	const isReadOnly = $derived(!!gitOpsManagedBy);
	let isSaving = $state(false);

	async function handleSave() {
		isSaving = true;
		try {
			await onSave();
			toast.success(m.container_compose_save_success());
			await invalidateAll();
		} catch (err: unknown) {
			const message = err instanceof Error ? err.message : m.container_compose_save_failed();
			toast.error(message);
		} finally {
			isSaving = false;
		}
	}
</script>

<div class="flex h-full min-h-0 flex-col gap-4 p-4">
	{#if gitOpsManagedBy}
		<Alert.Root variant="default">
			<AlertIcon class="size-4" />
			<Alert.Title>{m.container_compose_gitops_managed_title()}</Alert.Title>
			<Alert.Description>
				{m.container_compose_gitops_managed_description({ provider: gitOpsManagedBy })}
			</Alert.Description>
		</Alert.Root>
	{/if}

	<div class="bg-muted flex items-start gap-2 rounded-lg border px-4 py-3 text-sm">
		<span>
			{#if serviceName}
				{isReadOnly
					? m.container_compose_viewing_info({ file: fileTitle, project: projectName, service: serviceName })
					: m.container_compose_editing_info({ file: fileTitle, project: projectName, service: serviceName })}
			{:else}
				{isReadOnly
					? m.compose_editor_viewing_info({ file: fileTitle, project: projectName })
					: m.compose_editor_editing_info({ file: fileTitle, project: projectName })}
			{/if}
		</span>
	</div>

	<div class="flex min-h-0 flex-1 flex-col">
		{@render children()}
	</div>

	<div class="flex shrink-0 items-center gap-2">
		{#if !isReadOnly}
			<ArcaneButton action="save" loading={isSaving} disabled={!isDirty} onclick={handleSave} />
		{/if}
		<ArcaneButton
			action="base"
			href="/projects/{projectId}"
			icon={ExternalLinkIcon}
			customLabel={m.container_compose_view_project()}
		/>
	</div>
</div>

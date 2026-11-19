<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import type { Snippet } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertIcon, ExternalLinkIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		projectHref,
		projectName,
		gitOpsManagedBy = undefined,
		fileTitle,
		serviceName = undefined,
		isDirty,
		onSave,
		children
	}: {
		projectHref: string;
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
			const saved = await tryCatch(onSave());
			if (saved.error !== null) {
				toast.error(saved.error instanceof Error ? saved.error.message : m.container_compose_save_failed());
				return;
			}
			toast.success(m.container_compose_save_success());
			const refreshed = await tryCatch(refreshAll());
			if (refreshed.error !== null) {
				toast.error(refreshed.error instanceof Error ? refreshed.error.message : m.container_compose_save_failed());
			}
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

	<div class="flex items-start gap-2 rounded-lg border bg-muted px-4 py-3 text-sm">
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
		<ArcaneButton action="base" href={projectHref} icon={ExternalLinkIcon} customLabel={m.container_compose_view_project()} />
	</div>
</div>

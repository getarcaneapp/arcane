<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ContainerRegistryFormSheet from '#lib/components/sheets/container-registry-sheet.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { RegistryIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerRegistryListQuery, containerRegistryPullUsageQuery } from '#lib/query/container-registries.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import type { ContainerRegistry, ContainerRegistryPullUsage } from '#lib/types/docker.js';
	import type { ContainerRegistryCreate, ContainerRegistryUpdate } from '#lib/types/docker.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasShownPermission } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RegistryTable from './components/registry-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.registryRequestOptions));
	let isRegistryDialogOpen = $state(false);
	let isInfoDialogOpen = $state(false);
	let isRefreshing = $state(false);
	let registryToEdit = $state<ContainerRegistry | null>(null);
	const registriesQuery = createQuery(() => ({
		...containerRegistryListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const registries = $derived(registriesQuery.data ?? data.registries);
	const pullUsageQuery = createQuery(() => ({
		...containerRegistryPullUsageQuery(),
		initialData: data.pullUsage ?? undefined
	}));
	const pullUsageByRegistry = $derived.by<Record<string, ContainerRegistryPullUsage>>(() => {
		const entries = pullUsageQuery.data?.registries ?? [];
		return Object.fromEntries(entries.map((usage) => [usage.registryId, usage]));
	});

	const saveRegistryMutation = createMutation(() => ({
		mutationFn: ({
			registry,
			target
		}: {
			registry: ContainerRegistryCreate | ContainerRegistryUpdate;
			target: ContainerRegistry | null;
		}) =>
			target
				? containerRegistryService.updateRegistry(target.id, registry as ContainerRegistryUpdate)
				: containerRegistryService.createRegistry(registry as ContainerRegistryCreate),
		onSuccess: async (_result, { target }) => {
			toast.success(
				target
					? m.common_update_success({ resource: m.resource_registry() })
					: m.common_create_success({ resource: m.resource_registry() })
			);
			await queryClient.invalidateQueries({ queryKey: queryKeys.containerRegistries.all });
			isRegistryDialogOpen = false;
		},
		onError: (error) => {
			toast.error(m.registries_save_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	async function refreshRegistries() {
		isRefreshing = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				queryClient.invalidateQueries({ queryKey: queryKeys.containerRegistries.all }, { throwOnError: true })
			),
			message: m.common_refresh_failed({ resource: m.registries_title() }),
			setLoadingState: (value) => (isRefreshing = value),
			onSuccess: () => {
				toast.success(m.registries_refreshed());
			}
		});
	}

	function openCreateRegistryDialog() {
		registryToEdit = null;
		isRegistryDialogOpen = true;
	}

	function openEditRegistryDialog(registry: ContainerRegistry) {
		registryToEdit = registry;
		isRegistryDialogOpen = true;
	}

	function handleRegistryDialogSubmit({
		registry,
		isEditMode
	}: {
		registry: ContainerRegistryCreate | ContainerRegistryUpdate;
		isEditMode: boolean;
	}) {
		saveRegistryMutation.mutate({ registry, target: isEditMode && registryToEdit?.id ? registryToEdit : null });
	}

	const canCreateRegistry = $derived(hasShownPermission('registries:create'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [
			{
				id: 'info',
				action: 'inspect',
				label: m.registries_info_title(),
				onclick: () => (isInfoDialogOpen = true)
			}
		];
		if (canCreateRegistry) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.common_add_button({ resource: m.common_registry() }),
				onclick: openCreateRegistryDialog
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refreshRegistries,
			loading: isRefreshing,
			disabled: isRefreshing
		});
		return buttons;
	});
</script>

{#snippet registriesEmptyState()}
	<EmptyState
		variant="plain"
		icon={RegistryIcon}
		title={m.registries_empty_title()}
		description={m.registries_empty_description()}
		actionLabel={canCreateRegistry ? m.common_add_button({ resource: m.common_registry() }) : undefined}
		onAction={openCreateRegistryDialog}
	/>
{/snippet}

<ResourcePageLayout title={m.registries_title()} subtitle={m.registries_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<RegistryTable
			emptyState={registriesEmptyState}
			{registries}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			{pullUsageByRegistry}
			onEditRegistry={openEditRegistryDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#key registryToEdit?.id ?? 'new'}
			<ContainerRegistryFormSheet
				bind:open={isRegistryDialogOpen}
				{registryToEdit}
				onSubmit={handleRegistryDialogSubmit}
				isLoading={saveRegistryMutation.isPending}
			/>
		{/key}

		<Dialog.Root bind:open={isInfoDialogOpen}>
			<Dialog.Content class="max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.registries_info_title()}</Dialog.Title>
					<Dialog.Description>{m.registries_info_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="grid grid-cols-1 gap-6 py-4 md:grid-cols-2">
					<div class="space-y-3">
						<h4 class="text-sm font-medium">{m.registries_popular_public_title()}</h4>
						<div class="space-y-2 text-sm">
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_docker_hub()}</span>
								<code class="rounded-xl bg-muted px-2 py-1 text-xs">{m.registry_docker_hub_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_github_container_registry()}</span>
								<code class="rounded-xl bg-muted px-2 py-1 text-xs">{m.registry_github_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_google_container_registry()}</span>
								<code class="rounded-xl bg-muted px-2 py-1 text-xs">{m.registry_google_url()}</code>
							</div>
							<div class="flex justify-between">
								<span class="text-muted-foreground">{m.registry_quay_io()}</span>
								<code class="rounded-xl bg-muted px-2 py-1 text-xs">{m.registry_quay_url()}</code>
							</div>
						</div>
					</div>
					<div class="space-y-3">
						<h4 class="text-sm font-medium">{m.registries_auth_notes_title()}</h4>
						<div class="space-y-1 text-sm text-muted-foreground">
							<p>• {m.registries_auth_notes_bullet_docker_hub()}</p>
							<p>• {m.registries_auth_notes_bullet_github()}</p>
							<p>• {m.registries_auth_notes_bullet_anonymous()}</p>
							<p>• {m.registries_auth_notes_bullet_encrypted()}</p>
						</div>
					</div>
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</ResourcePageLayout>

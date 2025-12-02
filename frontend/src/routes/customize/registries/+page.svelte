<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import KeyIcon from '@lucide/svelte/icons/key';
	import { toast } from 'svelte-sonner';
	import type { ContainerRegistry } from '$lib/types/container-registry.type';
	import type { ContainerRegistryCreateDto, ContainerRegistryUpdateDto } from '$lib/types/container-registry.type';
	import ContainerRegistryFormSheet from '$lib/components/sheets/container-registry-sheet.svelte';
	import RegistryTable from './registry-table.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { containerRegistryService } from '$lib/services/container-registry-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let registries = $state(untrack(() => data.registries));
	let selectedIds = $state<string[]>([]);
	let isRegistryDialogOpen = $state(false);
	let registryToEdit = $state<ContainerRegistry | null>(null);
	let requestOptions = $state(untrack(() => data.registryRequestOptions));

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false
	});

	async function refreshRegistries() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(containerRegistryService.getRegistries(requestOptions)),
			message: m.common_refresh_failed({ resource: m.registries_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newRegistries) => {
				registries = newRegistries;
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

	async function handleRegistryDialogSubmit(detail: {
		registry: ContainerRegistryCreateDto | ContainerRegistryUpdateDto;
		isEditMode: boolean;
	}) {
		const { registry, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && registryToEdit?.id) {
				await containerRegistryService.updateRegistry(registryToEdit.id, registry as ContainerRegistryUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_registry() }));
			} else {
				await containerRegistryService.createRegistry(registry as ContainerRegistryCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_registry() }));
			}

			registries = await containerRegistryService.getRegistries(requestOptions);
			isRegistryDialogOpen = false;
		} catch (error) {
			console.error('Error saving registry:', error);
			toast.error(error instanceof Error ? error.message : m.registries_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const actionButtons: ActionButton[] = [
		{
			id: 'create',
			action: 'create',
			label: m.common_add_button({ resource: m.resource_registry_cap() }),
			onclick: openCreateRegistryDialog
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshRegistries,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	];
</script>

<ResourcePageLayout title={m.registries_title()} subtitle={m.registries_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Card.Root class="flex flex-col gap-6 border py-3 shadow-sm">
				<Card.Header
					class="@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 pb-4 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6"
				>
					<div class="flex items-center gap-3">
						<div class="rounded-full bg-green-500/10 p-2">
							<KeyIcon class="size-5 text-green-500" />
						</div>
						<div>
							<Card.Title>{m.registries_credentials_title()}</Card.Title>
							<Card.Description>
								{m.registries_credentials_description()}
							</Card.Description>
						</div>
					</div>
				</Card.Header>
				<Card.Content class="px-6">
					<RegistryTable bind:registries bind:selectedIds bind:requestOptions onEditRegistry={openEditRegistryDialog} />
				</Card.Content>
			</Card.Root>

			<Card.Root class="flex flex-col gap-6 border py-3 shadow-sm">
				<Card.Header
					class="@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6"
				>
					<Card.Title class="text-lg">{m.registries_info_title()}</Card.Title>
					<Card.Description>{m.registries_info_description()}</Card.Description>
				</Card.Header>
				<Card.Content class="px-6">
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<div class="space-y-3">
							<h4 class="text-sm font-medium">{m.registries_popular_public_title()}</h4>
							<div class="space-y-2 text-sm">
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.registry_docker_hub()}</span>
									<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_docker_hub_url()}</code>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.registry_github_container_registry()}</span>
									<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_github_url()}</code>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.registry_google_container_registry()}</span>
									<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_google_url()}</code>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.registry_quay_io()}</span>
									<code class="bg-muted rounded px-2 py-1 text-xs">{m.registry_quay_url()}</code>
								</div>
							</div>
						</div>
						<div class="space-y-3">
							<h4 class="text-sm font-medium">{m.registries_auth_notes_title()}</h4>
							<div class="text-muted-foreground space-y-1 text-sm">
								<p>• {m.registries_auth_notes_bullet_docker_hub()}</p>
								<p>• {m.registries_auth_notes_bullet_github()}</p>
								<p>• {m.registries_auth_notes_bullet_anonymous()}</p>
								<p>• {m.registries_auth_notes_bullet_encrypted()}</p>
							</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<ContainerRegistryFormSheet
			bind:open={isRegistryDialogOpen}
			bind:registryToEdit
			onSubmit={handleRegistryDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>
	{/snippet}
</ResourcePageLayout>

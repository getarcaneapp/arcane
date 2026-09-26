<script lang="ts">
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { UniversalMobileCard, type ColumnSpec } from '#lib/components/arcane-table/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ResourceDetailLayout, type DetailAction } from '#lib/layouts/index.js';
	import { AlertTriangleIcon, FolderOpenIcon, RegistryIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerRegistryService } from '#lib/services/container-registry-service.js';
	import type { RegistryRepository } from '#lib/types/docker.js';
	import type { Paginated } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();

	const registry = $derived(data.registry);
	const registryLabel = $derived(registry.url || 'docker.io');
	let repositories = $derived<Paginated<RegistryRepository> | null>(data.repositories);
	let repositoriesError = $derived<string | null>(data.repositoriesError);
	let requestOptions = $derived(data.requestOptions);
	let repositoryInput = $state('');
	let refreshing = $state(false);

	function repositoryUrl(name: string) {
		return `/customize/registries/${registry.id}/repository?name=${encodeURIComponent(name)}`;
	}

	function openRepository(event: SubmitEvent) {
		event.preventDefault();
		const name = repositoryInput.trim().replace(/^\/+|\/+$/g, '');
		if (name) goto(repositoryUrl(name));
	}

	async function loadRepositories(options = requestOptions) {
		const result = await tryCatch(containerRegistryService.getRepositories(registry.id, options));
		if (result.error !== null) {
			const message = extractApiErrorMessage(result.error);
			if (repositories) {
				toast.error(m.common_refresh_failed({ resource: m.registries_repositories() }), { description: message });
			} else {
				repositoriesError = message;
			}
			return repositories ?? { data: [], pagination: emptyPagination() };
		}
		repositoriesError = null;
		repositories = result.data;
		return result.data;
	}

	function emptyPagination() {
		return { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: requestOptions.pagination?.limit ?? 20 };
	}

	async function refresh() {
		refreshing = true;
		await loadRepositories();
		refreshing = false;
	}

	const actions: DetailAction[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			label: m.common_refresh(),
			loading: refreshing,
			disabled: refreshing,
			onclick: refresh
		}
	]);

	const columns = [
		{
			accessorKey: 'name',
			title: m.common_name(),
			sortable: true,
			cell: NameCell
		}
	] satisfies ColumnSpec<RegistryRepository>[];
</script>

{#snippet NameCell({ item }: { item: RegistryRepository })}
	<a href={repositoryUrl(item.name)} class="font-medium hover:underline">{item.name}</a>
{/snippet}

{#snippet RepositoryMobileCard({ item }: { item: RegistryRepository })}
	<UniversalMobileCard
		{item}
		icon={{ component: RegistryIcon, variant: 'purple' as const }}
		title={(item) => item.name}
		onclick={(item) => goto(repositoryUrl(item.name))}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: RegistryRepository })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(repositoryUrl(item.name))}>
			<FolderOpenIcon class="size-4" />
			{m.common_view()}
		</DropdownMenu.Item>
	</RowActionsMenu>
{/snippet}

<ResourceDetailLayout
	backUrl="/customize/registries"
	backLabel={m.registries_title()}
	title={m.registries_repositories()}
	subtitle={m.registries_repositories_subtitle({ url: registryLabel })}
	{actions}
>
	<div class="space-y-6">
		<form class="flex flex-col gap-2 sm:flex-row" onsubmit={openRepository}>
			<Input
				bind:value={repositoryInput}
				placeholder={m.registries_open_repository_placeholder()}
				aria-label={m.registries_open_repository()}
				class="sm:max-w-sm"
			/>
			<ArcaneButton
				action="inspect"
				type="submit"
				customLabel={m.registries_open_repository()}
				disabled={!repositoryInput.trim()}
			/>
		</form>

		{#if repositoriesError}
			<Alert.Root variant="destructive">
				<AlertTriangleIcon class="size-4" />
				<Alert.Title>{m.registries_catalog_unavailable_title()}</Alert.Title>
				<Alert.Description>
					<p>{m.registries_catalog_unavailable_description()}</p>
					<p class="font-mono text-xs">{repositoriesError}</p>
				</Alert.Description>
			</Alert.Root>

			{#if registry.repositoryNames?.length}
				<div class="space-y-2">
					<h3 class="text-sm font-medium">{m.registries_configured_repositories()}</h3>
					<ul class="space-y-1">
						{#each registry.repositoryNames as name (name)}
							<li>
								<a href={repositoryUrl(name)} class="font-mono text-sm hover:underline">{name}</a>
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		{:else if repositories}
			<ArcaneTable
				persistKey="arcane-registry-repositories-table"
				items={repositories}
				bind:requestOptions
				selectionDisabled
				onRefresh={loadRepositories}
				{columns}
				rowActions={RowActions}
				mobileCard={RepositoryMobileCard}
			/>
		{/if}
	</div>
</ResourceDetailLayout>

<script lang="ts">
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { EditIcon, TrashIcon, UsersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { oidcMappingService } from '#lib/services/oidc-mapping-service.js';
	import type { OidcGroupMapping } from '#lib/types/auth.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		mappings,
		onRefresh,
		onEdit
	}: {
		mappings: OidcGroupMapping[];
		onRefresh: () => Promise<void>;
		onEdit: (mapping: OidcGroupMapping) => void;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	const paginatedMappings = $derived.by<Paginated<OidcGroupMapping>>(() => ({
		data: mappings,
		pagination: {
			totalPages: 1,
			totalItems: mappings.length,
			currentPage: 1,
			itemsPerPage: Math.max(mappings.length, 1)
		}
	}));

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 1000 },
		sort: { column: 'claimValue', direction: 'asc' }
	});

	let selectedIds = $state<string[]>([]);

	async function handleDeleteMapping(mapping: OidcGroupMapping) {
		const safeClaim = mapping.claimValue?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.oidc_mappings_delete_title(),
			message: m.oidc_mappings_delete_message({ claim: safeClaim }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(oidcMappingService.delete(mapping.id)),
						message: m.oidc_mappings_delete_failed(),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.oidc_mappings_delete_success());
							await onRefresh();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ id: 'claimValue', accessorKey: 'claimValue', title: m.claim_value(), sortable: true, cell: ClaimCell },
		{ id: 'groupName', accessorKey: 'groupName', title: m.resource_group_cap(), sortable: true, cell: GroupCell }
	] satisfies ColumnSpec<OidcGroupMapping>[];

	const mobileFields = [{ id: 'groupName', label: m.resource_group_cap(), defaultVisible: true }];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ClaimCell({ item }: { item: OidcGroupMapping })}
	<div class="flex items-center gap-2">
		<code class="rounded bg-muted px-2 py-1 text-xs">{item.claimValue}</code>
		{#if item.source === 'env'}
			<Badge variant="amber" size="sm">ENV</Badge>
		{/if}
	</div>
{/snippet}

{#snippet GroupCell({ item }: { item: OidcGroupMapping })}
	<a href={`/settings/groups/${item.groupId}`} class="hover:underline">
		<Badge variant="blue" minWidth="20">{item.groupName}</Badge>
	</a>
{/snippet}

{#snippet OidcMappingMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: OidcGroupMapping;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: UsersIcon, variant: 'blue' }}
		title={(item: OidcGroupMapping) => item.claimValue}
		subtitle={() => null}
		badges={[
			(item: OidcGroupMapping) =>
				(mobileFieldVisibility['groupName'] ?? true) ? { variant: 'blue', text: item.groupName } : null
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: OidcGroupMapping })}
	<RowActionsMenu>
		<IfPermitted perm={PERMISSIONS.OIDCMAPPINGS_UPDATE}>
			<DropdownMenu.Item disabled={item.source === 'env'} onclick={() => onEdit(item)}>
				<EditIcon class="size-4" />
				{m.common_edit()}
			</DropdownMenu.Item>
		</IfPermitted>

		<IfPermitted perm={PERMISSIONS.OIDCMAPPINGS_DELETE}>
			<DropdownMenu.Separator />

			<DropdownMenu.Item
				variant="destructive"
				disabled={isLoading.removing || item.source === 'env'}
				onclick={() => handleDeleteMapping(item)}
			>
				<TrashIcon class="size-4" />
				{m.common_delete()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-oidc-mappings-table"
	items={paginatedMappings}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	selectionDisabled
	withoutPagination
	onRefresh={async () => {
		await onRefresh();
		return paginatedMappings;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={OidcMappingMobileCardSnippet}
/>

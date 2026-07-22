<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { Badge } from '$lib/components/ui/badge';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { OidcRoleMapping, Role } from '$lib/types/auth';
	import type { Environment } from '$lib/types/environment';
	import { BUILT_IN_ROLE_ADMIN, BUILT_IN_ROLE_EDITOR, BUILT_IN_ROLE_DEPLOYER, BUILT_IN_ROLE_VIEWER } from '$lib/types/auth';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { oidcMappingService } from '$lib/services/oidc-mapping-service';
	import { ShieldAlertIcon, TrashIcon, EditIcon } from '$lib/icons';

	let {
		mappings,
		roles,
		environments,
		onRefresh,
		onEdit
	}: {
		mappings: OidcRoleMapping[];
		roles: Role[];
		environments: Environment[];
		onRefresh: () => Promise<void>;
		onEdit: (mapping: OidcRoleMapping) => void;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	type BadgeVariant = 'red' | 'blue' | 'purple' | 'gray' | 'green' | 'amber';
	type IconVariant = 'emerald' | 'red' | 'amber' | 'blue' | 'purple' | 'gray' | 'sky' | 'orange';

	const rolesById = $derived.by(() => {
		const lookup: Record<string, Role> = {};
		for (const role of roles) lookup[role.id] = role;
		return lookup;
	});

	const envsById = $derived.by(() => {
		const lookup: Record<string, Environment> = {};
		for (const env of environments) lookup[env.id] = env;
		return lookup;
	});

	function getRoleBadgeVariant(role: Role | undefined): BadgeVariant {
		if (!role) return 'gray';
		if (!role.builtIn) return 'green';
		switch (role.id) {
			case BUILT_IN_ROLE_ADMIN:
				return 'red';
			case BUILT_IN_ROLE_EDITOR:
				return 'blue';
			case BUILT_IN_ROLE_DEPLOYER:
				return 'purple';
			case BUILT_IN_ROLE_VIEWER:
				return 'gray';
			default:
				return 'green';
		}
	}

	function getRoleIconVariant(role: Role | undefined): IconVariant {
		const v = getRoleBadgeVariant(role);
		return v === 'green' ? 'emerald' : v;
	}

	function getRoleName(roleId: string): string {
		return rolesById[roleId]?.name ?? roleId;
	}

	function getEnvName(environmentId: string | undefined): string {
		if (!environmentId) return m.global_org_wide();
		return envsById[environmentId]?.name ?? environmentId;
	}

	// ArcaneTable expects Paginated<T>; this list is unpaginated so we synthesize it.
	const paginatedMappings = $derived.by<Paginated<OidcRoleMapping>>(() => ({
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

	async function handleDeleteMapping(mapping: OidcRoleMapping) {
		const safeClaim = mapping.claimValue?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.oidc_mappings_delete_title(),
			message: m.oidc_mappings_delete_message({ claim: safeClaim }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
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
		{
			id: 'roleId',
			accessorKey: 'roleId',
			title: m.common_role(),
			sortable: true,
			cell: RoleCell
		},
		{
			id: 'environmentId',
			accessorKey: 'environmentId',
			title: m.resource_environment_cap(),
			sortable: true,
			cell: ScopeCell
		}
	] satisfies ColumnSpec<OidcRoleMapping>[];

	const mobileFields = [
		{ id: 'roleId', label: m.common_role(), defaultVisible: true },
		{ id: 'environmentId', label: m.resource_environment_cap(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ClaimCell({ item }: { item: OidcRoleMapping })}
	<div class="flex items-center gap-2">
		<code class="rounded bg-muted px-2 py-1 text-xs">{item.claimValue}</code>
		{#if item.source === 'env'}
			<Badge variant="amber" size="sm">ENV</Badge>
		{/if}
	</div>
{/snippet}

{#snippet RoleCell({ item }: { item: OidcRoleMapping })}
	{@const role = rolesById[item.roleId]}
	<Badge variant={getRoleBadgeVariant(role)} minWidth="20">{getRoleName(item.roleId)}</Badge>
{/snippet}

{#snippet ScopeCell({ item }: { item: OidcRoleMapping })}
	<span class={item.environmentId ? '' : 'text-muted-foreground italic'}>{getEnvName(item.environmentId)}</span>
{/snippet}

{#snippet OidcMappingMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: OidcRoleMapping;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	{@const role = rolesById[item.roleId]}
	<UniversalMobileCard
		{item}
		icon={{ component: ShieldAlertIcon, variant: getRoleIconVariant(role) }}
		title={(item: OidcRoleMapping) => item.claimValue}
		subtitle={() => null}
		badges={[
			(item: OidcRoleMapping) => ({
				variant: getRoleBadgeVariant(rolesById[item.roleId]),
				text: getRoleName(item.roleId)
			})
		]}
		fields={[
			{
				label: m.resource_environment_cap(),
				getValue: (item: OidcRoleMapping) => getEnvName(item.environmentId),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['environmentId'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: OidcRoleMapping })}
	<RowActionsMenu>
		<DropdownMenu.Item disabled={item.source === 'env'} onclick={() => onEdit(item)}>
			<EditIcon class="size-4" />
			{m.common_edit()}
		</DropdownMenu.Item>

		<DropdownMenu.Separator />

		<DropdownMenu.Item
			variant="destructive"
			disabled={isLoading.removing || item.source === 'env'}
			onclick={() => handleDeleteMapping(item)}
		>
			<TrashIcon class="size-4" />
			{m.common_delete()}
		</DropdownMenu.Item>
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

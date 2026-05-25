<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
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
	import { ShieldAlertIcon, TrashIcon, EditIcon, EllipsisIcon } from '$lib/icons';

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
		if (!environmentId) return m.users_role_assignments_scope_global();
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
			// TODO: i18n — add oidc_mappings_delete_title key
			title: 'Delete OIDC mapping?',
			message: m.oidc_mappings_delete_message({ claim: safeClaim }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(oidcMappingService.delete(mapping.id)),
						// TODO: i18n — add oidc_mappings_delete_failed key
						message: 'Failed to delete mapping',
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							// TODO: i18n — add oidc_mappings_delete_success key
							toast.success('Mapping deleted');
							await onRefresh();
						}
					});
				}
			}
		});
	}

	const columns = [
		{ id: 'claimValue', accessorKey: 'claimValue', title: m.oidc_mappings_col_claim(), sortable: true, cell: ClaimCell },
		{
			id: 'roleId',
			accessorKey: 'roleId',
			title: 'Role' /* TODO: i18n — add oidc_mappings_col_role key */,
			sortable: true,
			cell: RoleCell
		},
		{
			id: 'environmentId',
			accessorKey: 'environmentId',
			title: m.oidc_mappings_col_scope(),
			sortable: true,
			cell: ScopeCell
		}
	] satisfies ColumnSpec<OidcRoleMapping>[];

	const mobileFields = [
		{ id: 'roleId', label: 'Role' /* TODO: i18n — add oidc_mappings_col_role key */, defaultVisible: true },
		{ id: 'environmentId', label: m.oidc_mappings_col_scope(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet ClaimCell({ item }: { item: OidcRoleMapping })}
	<div class="flex items-center gap-2">
		<code class="bg-muted rounded px-2 py-1 text-xs">{item.claimValue}</code>
		{#if item.source === 'env'}
			<StatusBadge text="ENV" variant="amber" size="sm" minWidth="none" />
		{/if}
	</div>
{/snippet}

{#snippet RoleCell({ item }: { item: OidcRoleMapping })}
	{@const role = rolesById[item.roleId]}
	<StatusBadge text={getRoleName(item.roleId)} variant={getRoleBadgeVariant(role)} />
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
				label: m.oidc_mappings_col_scope(),
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
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
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
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
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

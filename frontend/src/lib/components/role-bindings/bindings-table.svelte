<script lang="ts">
	import { createQuery, keepPreviousData, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ShieldAlertIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { BindingSource, BindingSubjectType, Role, RoleBinding, RoleBindingFilter } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { roleBadgeVariant, selectorSummary } from '#lib/utils/rbac.js';

	import BindingSheet from './binding-sheet.svelte';

	const sourceLabels: Record<BindingSource, () => string> = {
		manual: m.bindings_source_manual,
		federated: m.bindings_source_federated,
		oidc: m.bindings_source_oidc
	};
	const sourceVariants: Record<BindingSource, 'gray' | 'purple' | 'sky'> = {
		manual: 'gray',
		federated: 'purple',
		oidc: 'sky'
	};

	let {
		subjectType,
		subjectId,
		subjectName,
		roles,
		environments,
		onChanged
	}: {
		subjectType: BindingSubjectType;
		subjectId: string;
		subjectName: string;
		roles: Role[] | null;
		environments: Environment[];
		onChanged?: () => void | Promise<void>;
	} = $props();

	const queryClient = useQueryClient();

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 20 },
		sort: { column: 'createdAt', direction: 'desc' }
	});
	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	let sheetOpen = $state(false);
	let removing = $state(false);

	const filter = $derived<RoleBindingFilter>({ subjectType, subjectId });

	const bindingsQuery = createQuery(() => {
		const options = requestOptions;
		const currentFilter = filter;
		return {
			queryKey: queryKeys.bindings.list(currentFilter, options),
			queryFn: () => roleService.getBindings(currentFilter, options),
			placeholderData: keepPreviousData
		};
	});

	const emptyPage: Paginated<RoleBinding> = {
		data: [],
		pagination: { totalPages: 0, totalItems: 0, currentPage: 1, itemsPerPage: 20 }
	};
	const bindings = $derived(bindingsQuery.data ?? emptyPage);

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.bindings.all });
		await onChanged?.();
	}

	function handleDelete(binding: RoleBinding) {
		confirmAndRun({
			title: m.bindings_delete_title(),
			message: m.bindings_delete_message({ role: binding.roleName, subject: subjectName }),
			confirmLabel: m.common_remove(),
			destructive: true,
			setLoading: (value) => (removing = value),
			run: () => roleService.deleteBinding(binding.id),
			failureMessage: m.common_remove_failed({ resource: m.resource_binding() }),
			onSuccess: async () => {
				toast.success(m.common_remove_success({ resource: m.resource_binding_cap() }));
				await refresh();
			}
		});
	}

	function scopeLabel(binding: RoleBinding): string {
		if (!binding.environmentId) return m.global_org_wide();
		return binding.environmentName || binding.environmentId;
	}

	const columns = [
		{ accessorKey: 'roleName', title: m.common_role(), sortable: true, cell: RoleCell },
		{ id: 'scope', accessorKey: 'environmentName', title: m.bindings_col_scope(), sortable: true, cell: ScopeCell },
		{ id: 'selector', title: m.bindings_col_resources(), cell: SelectorCell },
		{ accessorKey: 'source', title: m.bindings_col_source(), cell: SourceCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cellComponent: RelativeTimeCell }
	] satisfies ColumnSpec<RoleBinding>[];

	const mobileFields = [
		{ id: 'scope', label: m.bindings_col_scope(), defaultVisible: true },
		{ id: 'selector', label: m.bindings_col_resources(), defaultVisible: true },
		{ id: 'source', label: m.bindings_col_source(), defaultVisible: true }
	];
</script>

{#snippet RoleCell({ item }: { item: RoleBinding })}
	<Badge variant={roleBadgeVariant(item.roleId)} minWidth="20">{item.roleName}</Badge>
{/snippet}

{#snippet ScopeCell({ item }: { item: RoleBinding })}
	<span class={item.environmentId ? 'text-sm' : 'text-sm text-muted-foreground italic'}>{scopeLabel(item)}</span>
{/snippet}

{#snippet SelectorCell({ item }: { item: RoleBinding })}
	{@const summary = selectorSummary(item.selector)}
	{#if summary.length === 0}
		<span class="text-sm text-muted-foreground">{m.bindings_all_resources()}</span>
	{:else}
		<div class="flex flex-wrap gap-1">
			{#each summary as entry (entry)}
				<Badge variant="outline" size="sm" mono>{entry}</Badge>
			{/each}
		</div>
	{/if}
{/snippet}

{#snippet SourceCell({ item }: { item: RoleBinding })}
	<Badge variant={sourceVariants[item.source]} size="sm">
		{sourceLabels[item.source]()}
	</Badge>
{/snippet}

{#snippet RowActions({ item }: { item: RoleBinding })}
	<IfPermitted perm={PERMISSIONS.BINDINGS_DELETE}>
		<RowActionsMenu>
			<DropdownMenu.Item variant="destructive" disabled={removing} onclick={() => handleDelete(item)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

{#snippet BindingMobileCard({ item, mobileFieldVisibility }: { item: RoleBinding; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: ShieldAlertIcon, variant: 'gray' }}
		title={(item: RoleBinding) => item.roleName}
		subtitle={(item: RoleBinding) => ((mobileFieldVisibility['scope'] ?? true) ? scopeLabel(item) : null)}
		fields={[
			{
				label: m.bindings_col_resources(),
				getValue: (item: RoleBinding) => selectorSummary(item.selector).join(' · ') || m.bindings_all_resources(),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['selector'] ?? true
			},
			{
				label: m.bindings_col_source(),
				getValue: (item: RoleBinding) => sourceLabels[item.source](),
				icon: ShieldAlertIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['source'] ?? true
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

<div class="space-y-3">
	<div class="flex items-start justify-between gap-4">
		<div class="px-1">
			<h3 class="text-sm font-semibold">{m.bindings_title()}</h3>
			<p class="text-xs text-muted-foreground">{m.bindings_description({ subject: subjectName })}</p>
		</div>
		{#if roles}
			<IfPermitted perm={PERMISSIONS.BINDINGS_CREATE}>
				<ArcaneButton action="create" size="sm" onclick={() => (sheetOpen = true)} customLabel={m.bindings_add()} />
			</IfPermitted>
		{/if}
	</div>

	{#if bindingsQuery.isError}
		<p class="rounded-md border border-dashed p-4 text-sm text-destructive">
			{m.common_load_failed({ resource: m.bindings_title() })}: {extractApiErrorMessage(bindingsQuery.error)}
		</p>
	{:else}
		<ArcaneTable
			persistKey={`arcane-bindings-${subjectType}-table`}
			items={bindings}
			bind:requestOptions
			bind:selectedIds
			bind:mobileFieldVisibility
			selectionDisabled
			withoutSearch
			onRefresh={async (options) => {
				requestOptions = options;
				await bindingsQuery.refetch();
				return bindings;
			}}
			{columns}
			{mobileFields}
			rowActions={RowActions}
			mobileCard={BindingMobileCard}
		/>
	{/if}
</div>

{#if roles}
	<BindingSheet bind:open={sheetOpen} {subjectType} {subjectId} {subjectName} {roles} {environments} onCreated={refresh} />
{/if}

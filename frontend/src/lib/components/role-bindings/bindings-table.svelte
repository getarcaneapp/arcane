<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import RelativeTimeCell from '#lib/components/arcane-table/cells/relative-time-cell.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { roleService } from '#lib/services/role-service.js';
	import type { BindingSource, BindingSubjectType, Role, RoleBinding, RoleBindingFilter } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { roleBadgeVariant } from '#lib/utils/rbac.js';

	import BindingSheet from './binding-sheet.svelte';

	const sourceLabels: Record<BindingSource, () => string> = {
		manual: m.bindings_source_manual,
		federated: m.bindings_source_federated,
		oidc: m.bindings_source_oidc,
		env: m.bindings_source_env
	};
	const sourceVariants: Record<BindingSource, 'gray' | 'purple' | 'sky' | 'amber'> = {
		manual: 'gray',
		federated: 'purple',
		oidc: 'sky',
		env: 'amber'
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

	// A subject has few bindings, so load them all and skip pagination chrome.
	const listOptions: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: -1 },
		sort: { column: 'createdAt', direction: 'asc' }
	};

	let sheetOpen = $state(false);
	let removing = $state(false);

	const filter = $derived<RoleBindingFilter>({ subjectType, subjectId });

	const bindingsQuery = createQuery(() => {
		const currentFilter = filter;
		return {
			queryKey: queryKeys.bindings.list(currentFilter, listOptions),
			queryFn: () => roleService.getBindings(currentFilter, listOptions)
		};
	});

	const bindings = $derived(bindingsQuery.data?.data ?? []);

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
</script>

{#snippet RowActions(item: RoleBinding)}
	<IfPermitted perm={PERMISSIONS.BINDINGS_DELETE}>
		<RowActionsMenu>
			{#if item.source === 'env'}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger class="w-full" disabledChild>
						<DropdownMenu.Item variant="destructive" disabled>
							<TrashIcon class="size-4" />
							{m.common_remove()}
						</DropdownMenu.Item>
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content>
						<p class="max-w-xs text-xs break-words">{m.bindings_env_managed_description()}</p>
					</ArcaneTooltip.Content>
				</ArcaneTooltip.Root>
			{:else}
				<DropdownMenu.Item variant="destructive" disabled={removing} onclick={() => handleDelete(item)}>
					<TrashIcon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			{/if}
		</RowActionsMenu>
	</IfPermitted>
{/snippet}

<div class="space-y-3">
	<div class="flex items-start justify-between gap-4">
		<div>
			<h3 class="text-sm font-semibold">{m.bindings_title()}</h3>
			<p class="text-xs text-muted-foreground">{m.bindings_description({ subject: subjectName })}</p>
		</div>
		{#if roles}
			<IfPermitted perm={PERMISSIONS.BINDINGS_CREATE}>
				<ArcaneButton action="create" size="sm" onclick={() => (sheetOpen = true)} customLabel={m.bindings_add()} />
			</IfPermitted>
		{/if}
	</div>

	{#if bindingsQuery.isPending}
		<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
	{:else if bindingsQuery.isError}
		<p class="rounded-md border border-dashed p-4 text-sm text-destructive">
			{m.common_load_failed({ resource: m.bindings_title() })}: {extractApiErrorMessage(bindingsQuery.error)}
		</p>
	{:else if bindings.length === 0}
		<p class="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
			{m.bindings_empty({ subject: subjectName })}
		</p>
	{:else}
		<div class="overflow-x-auto rounded-md border">
			<Table.Root aria-label={m.bindings_title()}>
				<Table.Header>
					<Table.Row>
						<Table.Head scope="col">{m.common_role()}</Table.Head>
						<Table.Head scope="col">{m.bindings_col_scope()}</Table.Head>
						<Table.Head scope="col">{m.bindings_col_source()}</Table.Head>
						<Table.Head scope="col">{m.common_created()}</Table.Head>
						<Table.Head scope="col" class="w-0"><span class="sr-only">{m.common_actions()}</span></Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each bindings as binding (binding.id)}
						<Table.Row>
							<Table.Cell>
								<Badge variant={roleBadgeVariant(binding.roleId)} minWidth="20">{binding.roleName}</Badge>
							</Table.Cell>
							<Table.Cell>
								<span class={binding.environmentId ? 'text-sm' : 'text-sm text-muted-foreground italic'}
									>{scopeLabel(binding)}</span
								>
							</Table.Cell>
							<Table.Cell>
								<Badge variant={sourceVariants[binding.source]} size="sm">{sourceLabels[binding.source]()}</Badge>
							</Table.Cell>
							<Table.Cell><RelativeTimeCell value={binding.createdAt} /></Table.Cell>
							<Table.Cell class="text-right">{@render RowActions(binding)}</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</div>

{#if roles}
	<BindingSheet bind:open={sheetOpen} {subjectType} {subjectId} {subjectName} {roles} {environments} onCreated={refresh} />
{/if}

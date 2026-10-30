<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import VariableFormSheet from '#lib/components/sheets/variable-form-sheet.svelte';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { variableListQuery } from '#lib/query/variables.js';
	import { variableService } from '#lib/services/variable-service.js';
	import { GLOBAL_SCOPE } from '#lib/types/auth.js';
	import type { GlobalVariable, GlobalVariableCreate, GlobalVariableUpdate, VariableEnvSyncResult } from '#lib/types/variable.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import VariableTable from './components/variable-table.svelte';

	type VariableFormPayload =
		| { mode: 'create'; variable: GlobalVariableCreate }
		| { mode: 'edit'; id: string; variable: GlobalVariableUpdate }
		| { mode: 'bulk'; variables: GlobalVariableCreate[] };

	let { data } = $props();

	const queryClient = useQueryClient();

	const variablesQuery = createQuery(() => variableListQuery());
	const variables = $derived(variablesQuery.data ?? data.variables);
	let isSheetOpen = $state(false);
	let variableSession = $state(0);
	let variableToEdit = $state<GlobalVariable | null>(null);

	const canCreateVariables = $derived(hasPermission('variables:create', GLOBAL_SCOPE));

	function reportSyncResults(results?: VariableEnvSyncResult[]) {
		const failed = results?.filter((result) => result.status === 'error') ?? [];
		if (failed.length > 0) {
			toast.warning(
				m.sync_failed_environments({
					envs: failed.map((result) => result.environmentName || result.environmentId).join(', ')
				})
			);
		}
	}

	function openCreateSheet() {
		variableToEdit = null;
		variableSession += 1;
		isSheetOpen = true;
	}

	function openEditSheet(variable: GlobalVariable) {
		variableToEdit = variable;
		variableSession += 1;
		isSheetOpen = true;
	}

	const saveVariableMutation = createMutation(() => ({
		mutationFn: (payload: VariableFormPayload) => {
			switch (payload.mode) {
				case 'edit':
					return variableService.update(payload.id, payload.variable);
				case 'bulk':
					return variableService.createMany(payload.variables);
				default:
					return variableService.create(payload.variable);
			}
		},
		onSuccess: async (response, payload) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.variables.all });
			if (payload.mode === 'bulk') {
				toast.success(m.count_variables_created({ count: payload.variables.length }));
			} else if (payload.mode === 'edit') {
				toast.success(m.common_update_success({ resource: m.variable() }));
			} else {
				toast.success(m.common_create_success({ resource: m.variable() }));
			}
			reportSyncResults(response?.syncResults);
			isSheetOpen = false;
		},
		onError: async (error, payload) => {
			toast.error(
				payload.mode === 'edit'
					? m.common_update_failed({ resource: m.variable() })
					: m.common_create_failed({ resource: m.variable() }),
				{ description: extractApiErrorMessage(error) }
			);
			await queryClient.invalidateQueries({ queryKey: queryKeys.variables.all });
		}
	}));

	const deleteVariableMutation = createMutation(() => ({
		mutationFn: (variable: GlobalVariable) => variableService.delete(variable.id),
		onSuccess: async (response) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.variables.all });
			toast.success(m.common_delete_success({ resource: m.variable() }));
			reportSyncResults(response?.syncResults);
		},
		onError: async (error) => {
			toast.error(m.common_delete_failed({ resource: m.variable() }), { description: extractApiErrorMessage(error) });
			await queryClient.invalidateQueries({ queryKey: queryKeys.variables.all });
		}
	}));

	async function handleSheetSubmit(payload: VariableFormPayload): Promise<Error | null> {
		const result = await tryCatch(saveVariableMutation.mutateAsync(payload));
		return result.error;
	}

	function handleDelete(variable: GlobalVariable) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.variable() }),
			message: m.common_delete_confirm({ resource: variable.key }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await tryCatch(deleteVariableMutation.mutateAsync(variable));
				}
			}
		});
	}

	const actionButtons = $derived<ActionButton[]>(
		canCreateVariables
			? [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_add_button({ resource: m.variable() }),
						onclick: openCreateSheet
					}
				]
			: []
	);
</script>

<ResourcePageLayout title={m.variables_title()} subtitle={m.variables_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<VariableTable {variables} environments={data.environments} onEdit={openEditSheet} onDelete={handleDelete} />
	{/snippet}

	{#snippet additionalContent()}
		{#if variableSession > 0}
			{#key variableSession}
				<VariableFormSheet
					bind:open={isSheetOpen}
					bind:variableToEdit
					environments={data.environments}
					existingVariables={variables}
					isLoading={saveVariableMutation.isPending}
					onSubmit={handleSheetSubmit}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>

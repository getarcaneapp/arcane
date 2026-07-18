<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import VariableFormSheet from '$lib/components/sheets/variable-form-sheet.svelte';
	import VariableTable from './variable-table.svelte';
	import { variableService } from '$lib/services/variable-service';
	import type {
		GlobalVariable,
		GlobalVariableCreateDto,
		GlobalVariableUpdateDto,
		VariableEnvSyncResult
	} from '$lib/types/variable';
	import { m } from '$lib/paraglide/messages';
	import { hasPermission } from '$lib/utils/auth';

	type VariableFormPayload =
		| { mode: 'create'; variable: GlobalVariableCreateDto }
		| { mode: 'edit'; id: string; variable: GlobalVariableUpdateDto }
		| { mode: 'bulk'; variables: GlobalVariableCreateDto[] };

	let { data } = $props();

	let variables = $state(untrack(() => data.variables));
	let isSheetOpen = $state(false);
	let variableToEdit = $state<GlobalVariable | null>(null);
	let isSubmitting = $state(false);

	const canManageVariables = $derived(hasPermission('templates:update'));

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
		isSheetOpen = true;
	}

	function openEditSheet(variable: GlobalVariable) {
		variableToEdit = variable;
		isSheetOpen = true;
	}

	async function handleSheetSubmit(payload: VariableFormPayload) {
		isSubmitting = true;
		try {
			let response;
			if (payload.mode === 'edit') {
				response = await variableService.update(payload.id, payload.variable);
			} else if (payload.mode === 'bulk') {
				response = await variableService.createMany(payload.variables);
			} else {
				response = await variableService.create(payload.variable);
			}

			variables = await variableService.list();

			if (payload.mode === 'bulk') {
				toast.success(m.count_variables_created({ count: payload.variables.length }));
			} else if (payload.mode === 'edit') {
				toast.success(m.common_update_success({ resource: m.variable() }));
			} else {
				toast.success(m.common_create_success({ resource: m.variable() }));
			}
			reportSyncResults(response?.syncResults);

			isSheetOpen = false;
			variableToEdit = null;
		} catch (error) {
			console.error('Error saving variable:', error);
			toast.error(
				payload.mode === 'edit'
					? m.common_update_failed({ resource: m.variable() })
					: m.common_create_failed({ resource: m.variable() })
			);
			// A bulk create can partially succeed before the failing entry, so
			// refresh even on error to keep the table and duplicate-key
			// validation in sync with what was actually persisted.
			variables = await variableService.list().catch(() => variables);
		} finally {
			isSubmitting = false;
		}
	}

	function handleDelete(variable: GlobalVariable) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.variable() }),
			message: m.common_delete_confirm({ resource: variable.key }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					try {
						const response = await variableService.delete(variable.id);
						variables = await variableService.list();
						toast.success(m.common_delete_success({ resource: m.variable() }));
						reportSyncResults(response?.syncResults);
					} catch (error) {
						console.error('Error deleting variable:', error);
						toast.error(m.common_delete_failed({ resource: m.variable() }));
						variables = await variableService.list().catch(() => variables);
					}
				}
			}
		});
	}

	const actionButtons = $derived<ActionButton[]>(
		canManageVariables
			? [
					{
						id: 'create',
						action: 'create',
						label: m.common_add_button({ resource: m.variable() }),
						onclick: openCreateSheet
					}
				]
			: []
	);
</script>

<ResourcePageLayout title={m.variables_title()} subtitle={m.variables_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<VariableTable bind:variables environments={data.environments} onEdit={openEditSheet} onDelete={handleDelete} />
	{/snippet}

	{#snippet additionalContent()}
		<VariableFormSheet
			bind:open={isSheetOpen}
			bind:variableToEdit
			environments={data.environments}
			existingVariables={variables}
			isLoading={isSubmitting}
			onSubmit={handleSheetSubmit}
		/>
	{/snippet}
</ResourcePageLayout>

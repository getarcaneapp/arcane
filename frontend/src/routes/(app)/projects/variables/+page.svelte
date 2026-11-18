<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import SecretSourceFormSheet from '#lib/components/sheets/secret-source-form-sheet.svelte';
	import VariableFormSheet from '#lib/components/sheets/variable-form-sheet.svelte';
	import { TabBar } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import { ApiKeyIcon, VariableIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceListQuery } from '#lib/query/secret-sources.js';
	import { variableListQuery } from '#lib/query/variables.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import { variableService } from '#lib/services/variable-service.js';
	import type { GlobalVariable, EnvironmentSyncStatus } from '#lib/types/env.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SecretSource, SecretSourceInput, SecretSourceUpdate } from '#lib/types/secretsource.js';
	import type { GlobalVariableCreate, GlobalVariableUpdate } from '#lib/types/ui/env.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SecretSourceTable from './components/secret-source-table.svelte';
	import VariableTable from './components/variable-table.svelte';

	type VariableFormPayload =
		| { mode: 'create'; variable: GlobalVariableCreate }
		| { mode: 'edit'; id: string; variable: GlobalVariableUpdate }
		| { mode: 'bulk'; variables: GlobalVariableCreate[] };

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceInput }
		| { mode: 'edit'; id: string; source: SecretSourceUpdate };

	let { data } = $props();

	const queryClient = useQueryClient();

	const variablesQuery = createQuery(() => ({ ...variableListQuery(), enabled: data.showVariables }));
	const variables = $derived(variablesQuery.data ?? data.variables);
	let isSheetOpen = $state(false);
	let variableSession = $state(0);
	let variableToEdit = $state<GlobalVariable | null>(null);

	const sourcesQuery = createQuery(() => ({ ...secretSourceListQuery(), enabled: data.showSecretSources }));
	const sources = $derived(sourcesQuery.data ?? data.sources);
	let isSourceSheetOpen = $state(false);
	let sourceSession = $state(0);
	let sourceToEdit = $state<SecretSource | null>(null);
	const refreshingIds = $state<Record<string, boolean>>({});

	const showTabs = $derived(data.showVariables && data.showSecretSources);
	const urlTab = useUrlTab({
		tabs: () => [
			{ value: 'variables', label: m.variables_title(), icon: VariableIcon, visible: data.showVariables },
			{ value: 'secrets', label: m.external_secrets(), icon: ApiKeyIcon, visible: data.showSecretSources }
		],
		defaultTab: () => 'variables',
		ready: () => showTabs
	});

	const canCreateVariables = $derived(hasPermission('variables:create', GLOBAL_SCOPE));
	const canCreateSources = $derived(hasPermission('secret-sources:create', GLOBAL_SCOPE));

	function reportSyncResults(results?: EnvironmentSyncStatus[]) {
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

	function openCreateSourceSheet() {
		sourceToEdit = null;
		sourceSession += 1;
		isSourceSheetOpen = true;
	}

	function openEditSourceSheet(source: SecretSource) {
		sourceToEdit = source;
		sourceSession += 1;
		isSourceSheetOpen = true;
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

	const saveSourceMutation = createMutation(() => ({
		mutationFn: (payload: SecretSourceFormPayload) =>
			payload.mode === 'edit'
				? secretSourceService.update(payload.id, payload.source)
				: secretSourceService.create(payload.source),
		onSuccess: async (response, payload) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
			toast.success(
				payload.mode === 'edit'
					? m.common_update_success({ resource: m.external_secret() })
					: m.common_create_success({ resource: m.external_secret() })
			);
			reportSyncResults(response?.syncResults);
			isSourceSheetOpen = false;
		},
		onError: async (error, payload) => {
			toast.error(
				payload.mode === 'edit'
					? m.common_update_failed({ resource: m.external_secret() })
					: m.common_create_failed({ resource: m.external_secret() }),
				{ description: extractApiErrorMessage(error) }
			);
			await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
		}
	}));

	const deleteSourceMutation = createMutation(() => ({
		mutationFn: (source: SecretSource) => secretSourceService.delete(source.id),
		onSuccess: async (response) => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
			toast.success(m.common_delete_success({ resource: m.external_secret() }));
			reportSyncResults(response?.syncResults);
		},
		onError: async (error) => {
			toast.error(m.common_delete_failed({ resource: m.external_secret() }), { description: extractApiErrorMessage(error) });
			await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
		}
	}));

	async function handleSheetSubmit(payload: VariableFormPayload): Promise<Error | null> {
		const result = await tryCatch(saveVariableMutation.mutateAsync(payload));
		return result.error;
	}

	async function handleSourceSheetSubmit(payload: SecretSourceFormPayload): Promise<Error | null> {
		const result = await tryCatch(saveSourceMutation.mutateAsync(payload));
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

	function handleSourceDelete(source: SecretSource) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.external_secret() }),
			message: m.common_delete_confirm({ resource: source.name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await tryCatch(deleteSourceMutation.mutateAsync(source));
				}
			}
		});
	}

	async function handleSourceRefresh(source: SecretSource) {
		if (refreshingIds[source.id]) return;
		refreshingIds[source.id] = true;
		const result = await tryCatch(secretSourceService.refresh(source.id));
		refreshingIds[source.id] = false;
		await handleApiResultWithCallbacks({
			result,
			message: m.common_refresh_failed({ resource: source.name }),
			onSuccess: (response) => {
				toast.success(m.common_refresh_success({ resource: source.name }));
				reportSyncResults(response.syncResults);
			}
		});
		await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
	}

	const actionButtons = $derived.by<ActionButton[]>(() => {
		if (urlTab.value === 'secrets') {
			return canCreateSources
				? [
						{
							id: 'create',
							action: 'create',
							placement: 'primary',
							label: m.common_add_button({ resource: m.external_secret() }),
							onclick: openCreateSourceSheet
						}
					]
				: [];
		}
		return canCreateVariables
			? [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_add_button({ resource: m.variable() }),
						onclick: openCreateSheet
					}
				]
			: [];
	});
</script>

{#snippet variablesContent()}
	<VariableTable {variables} environments={data.environments} onEdit={openEditSheet} onDelete={handleDelete} />
{/snippet}

{#snippet secretsContent()}
	<SecretSourceTable
		{sources}
		environments={data.environments}
		{refreshingIds}
		onEdit={openEditSourceSheet}
		onRefresh={handleSourceRefresh}
		onDelete={handleSourceDelete}
	/>
{/snippet}

<ResourcePageLayout
	title={m.variables_and_secrets()}
	subtitle={data.showSecretSources ? m.variables_and_secrets_subtitle() : m.variables_subtitle()}
	{actionButtons}
>
	{#snippet mainContent()}
		{#if showTabs}
			<Tabs.Root value={urlTab.value}>
				<div class="pb-6">
					<TabBar items={urlTab.items} onValueChange={urlTab.select} />
				</div>

				<Tabs.Content value="variables">
					{@render variablesContent()}
				</Tabs.Content>

				<Tabs.Content value="secrets">
					{@render secretsContent()}
				</Tabs.Content>
			</Tabs.Root>
		{:else if data.showVariables}
			{@render variablesContent()}
		{:else}
			{@render secretsContent()}
		{/if}
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
		{#if sourceSession > 0}
			{#key sourceSession}
				<SecretSourceFormSheet
					bind:open={isSourceSheetOpen}
					{sourceToEdit}
					environments={data.environments}
					isLoading={saveSourceMutation.isPending}
					onSubmit={handleSourceSheetSubmit}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>

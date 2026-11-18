<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import SecretSourceFormSheet from '#lib/components/sheets/secret-source-form-sheet.svelte';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { secretSourceListQuery } from '#lib/query/secret-sources.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { EnvironmentSyncStatus } from '#lib/types/env.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SecretSource, SecretSourceInput, SecretSourceUpdate } from '#lib/types/secretsource.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SecretSourceTable from './components/secret-source-table.svelte';

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceInput }
		| { mode: 'edit'; id: string; source: SecretSourceUpdate };

	let { data } = $props();

	const queryClient = useQueryClient();

	const sourcesQuery = createQuery(() => secretSourceListQuery());
	const sources = $derived(sourcesQuery.data ?? data.sources);
	let isSheetOpen = $state(false);
	let sourceSession = $state(0);
	let sourceToEdit = $state<SecretSource | null>(null);
	const refreshingIds = $state<Record<string, boolean>>({});

	const canCreate = $derived(hasPermission('secret-sources:create', GLOBAL_SCOPE));

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
		sourceToEdit = null;
		sourceSession += 1;
		isSheetOpen = true;
	}

	function openEditSheet(source: SecretSource) {
		sourceToEdit = source;
		sourceSession += 1;
		isSheetOpen = true;
	}

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
			isSheetOpen = false;
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

	async function handleSheetSubmit(payload: SecretSourceFormPayload): Promise<Error | null> {
		const result = await tryCatch(saveSourceMutation.mutateAsync(payload));
		return result.error;
	}

	function handleDelete(source: SecretSource) {
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

	async function handleRefresh(source: SecretSource) {
		if (refreshingIds[source.id]) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(secretSourceService.refresh(source.id)),
			message: m.common_refresh_failed({ resource: source.name }),
			setLoadingState: (value) => {
				refreshingIds[source.id] = value;
			},
			onSuccess: (response) => {
				toast.success(m.common_refresh_success({ resource: source.name }));
				reportSyncResults(response.syncResults);
			}
		});
		await queryClient.invalidateQueries({ queryKey: queryKeys.secretSources.all });
	}

	const actionButtons = $derived<ActionButton[]>(
		canCreate
			? [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_add_button({ resource: m.external_secret() }),
						onclick: openCreateSheet
					}
				]
			: []
	);
</script>

<ResourcePageLayout title={m.external_secrets()} subtitle={m.external_secret_form_description()} {actionButtons}>
	{#snippet mainContent()}
		<SecretSourceTable
			{sources}
			environments={data.environments}
			{refreshingIds}
			onEdit={openEditSheet}
			onRefresh={handleRefresh}
			onDelete={handleDelete}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if sourceSession > 0}
			{#key sourceSession}
				<SecretSourceFormSheet
					bind:open={isSheetOpen}
					{sourceToEdit}
					environments={data.environments}
					isLoading={saveSourceMutation.isPending}
					onSubmit={handleSheetSubmit}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>

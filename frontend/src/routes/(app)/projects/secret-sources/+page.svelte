<script lang="ts">
	import { toast } from 'svelte-sonner';

	import SecretSourceSheet from '#lib/components/sheets/secret-source-sheet.svelte';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { LockIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SecretSource, SecretSourceCreateRequest, SecretSourceUpdateRequest } from '#lib/types/secretsource.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { confirmAndRun } from '#lib/utils/bulk-actions.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SecretSourceTable from './components/secret-source-table.svelte';

	type SecretSourceFormPayload =
		| { mode: 'create'; source: SecretSourceCreateRequest }
		| { mode: 'edit'; id: string; source: SecretSourceUpdateRequest };

	let { data } = $props();

	let sources = $derived(data.sources);
	let isSheetOpen = $state(false);
	let sheetSession = $state(0);
	let sourceToEdit = $state<SecretSource | null>(null);
	let isSubmitting = $state(false);

	const canCreate = $derived(hasPermission('secret-sources:create', GLOBAL_SCOPE));

	function openCreateSheet() {
		sourceToEdit = null;
		sheetSession += 1;
		isSheetOpen = true;
	}

	function openEditSheet(source: SecretSource) {
		sourceToEdit = source;
		sheetSession += 1;
		isSheetOpen = true;
	}

	async function refreshSources() {
		const result = await tryCatch(secretSourceService.list());
		if (!result.error) sources = result.data;
	}

	async function handleSheetSubmit(payload: SecretSourceFormPayload) {
		const isEdit = payload.mode === 'edit';
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				isEdit ? secretSourceService.update(payload.id, payload.source) : secretSourceService.create(payload.source)
			),
			message: isEdit
				? m.common_update_failed({ resource: m.secret_source() })
				: m.common_create_failed({ resource: m.secret_source() }),
			setLoadingState: (value) => (isSubmitting = value),
			onSuccess: async (saved) => {
				toast.success(
					isEdit
						? m.common_update_success({ resource: m.secret_source() })
						: m.common_create_success({ resource: m.secret_source() })
				);
				isSheetOpen = false;
				// Test the saved settings so the table shows a current status.
				await recordConnectionTest(saved);
				await refreshSources();
			}
		});
	}

	function storedTestRequest(source: SecretSource) {
		return { sourceId: source.id, provider: source.provider, settings: source.settings };
	}

	async function recordConnectionTest(source: SecretSource) {
		await tryCatch(secretSourceService.test(storedTestRequest(source)));
	}

	async function handleTest(source: SecretSource) {
		const result = await tryCatch(secretSourceService.test(storedTestRequest(source)));
		await handleApiResultWithCallbacks({
			result,
			message: m.secret_sources_test_failed(),
			onSuccess: async (outcome) => {
				if (outcome.ok) {
					toast.success(outcome.message);
				} else {
					toast.error(m.secret_sources_test_failed(), { description: outcome.message });
				}
				await refreshSources();
			}
		});
	}

	function handleDelete(source: SecretSource) {
		confirmAndRun({
			title: m.common_delete_title({ resource: m.secret_source() }),
			message: m.common_delete_confirm({ resource: source.name }),
			confirmLabel: m.common_delete(),
			destructive: true,
			run: () => secretSourceService.delete(source.id),
			failureMessage: m.common_delete_failed({ resource: m.secret_source() }),
			onSuccess: async () => {
				toast.success(m.common_delete_success({ resource: m.secret_source() }));
				await refreshSources();
			}
		});
	}

	const actionButtons = $derived<ActionButton[]>(
		canCreate
			? [
					{
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_add_button({ resource: m.secret_source() }),
						onclick: openCreateSheet
					}
				]
			: []
	);
</script>

<ResourcePageLayout title={m.secret_sources_title()} subtitle={m.secret_sources_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		{#if sources.length === 0}
			<div class="rounded-xl border border-dashed border-border/70">
				<Empty.Root>
					<Empty.Header>
						<Empty.Media variant="icon">
							<LockIcon />
						</Empty.Media>
						<Empty.Title>{m.secret_sources_empty_title()}</Empty.Title>
						<Empty.Description>{m.secret_sources_empty_description()}</Empty.Description>
					</Empty.Header>
				</Empty.Root>
			</div>
		{:else}
			<SecretSourceTable {sources} onEdit={openEditSheet} onDelete={handleDelete} onTest={handleTest} />
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		{#if sheetSession > 0}
			{#key sheetSession}
				<SecretSourceSheet bind:open={isSheetOpen} {sourceToEdit} isLoading={isSubmitting} onSubmit={handleSheetSubmit} />
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>

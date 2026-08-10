<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog';
	import SnippetFormSheet from '#lib/components/sheets/snippet-form-sheet.svelte';
	import SnippetTable from './snippet-table.svelte';
	import SnippetRunSheet from './snippet-run-sheet.svelte';
	import { snippetService } from '#lib/services/snippet-service';
	import type { Snippet, SnippetCreateDto, SnippetUpdateDto } from '#lib/types/snippet';
	import { m } from '#lib/paraglide/messages';
	import { hasPermission } from '#lib/utils/auth';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte';
	import { queryKeys } from '#lib/query/query-keys';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import type { SearchPaginationSortRequest } from '#lib/types/shared';

	type SnippetFormPayload =
		| { mode: 'create'; snippet: SnippetCreateDto }
		| { mode: 'edit'; id: string; snippet: SnippetUpdateDto };

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(
		untrack(() => data.snippets),
		untrack(() => data.snippetRequestOptions)
	);
	let previousEnvId = untrack(() => pageState.envId);

	const snippetsQuery = createQuery(() => {
		const queryEnvId = pageState.envId;
		return {
			queryKey: queryKeys.snippets.list(queryEnvId, pageState.requestOptions),
			queryFn: () => snippetService.getSnippets(queryEnvId, pageState.requestOptions),
			initialData: data.envId === queryEnvId ? data.snippets : undefined,
			select: (value: Awaited<ReturnType<typeof snippetService.getSnippets>>) => ({ envId: queryEnvId, value })
		};
	});
	let displayedEnvId = $state<string | null>(untrack(() => (data.envId === pageState.envId ? data.envId : null)));
	const resourcesReady = $derived(displayedEnvId === pageState.envId);

	let isFormSheetOpen = $state(false);
	let snippetToEdit = $state<Snippet | null>(null);
	let isSubmitting = $state(false);

	let isRunSheetOpen = $state(false);
	let snippetToRun = $state<Snippet | null>(null);

	$effect(() => {
		if (snippetsQuery.data?.envId === pageState.envId) {
			pageState.items = snippetsQuery.data.value;
			displayedEnvId = pageState.envId;
		}
	});

	$effect(() => {
		if (pageState.envId === previousEnvId) return;
		previousEnvId = pageState.envId;
		displayedEnvId = null;
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function loadSnippets(options: SearchPaginationSortRequest = pageState.requestOptions, requestedEnvId = pageState.envId) {
		pageState.requestOptions = options;
		const next = await queryClient.fetchQuery({
			queryKey: queryKeys.snippets.list(requestedEnvId, options),
			queryFn: () => snippetService.getSnippets(requestedEnvId, options)
		});
		if (requestedEnvId !== pageState.envId) return;
		pageState.items = next;
		displayedEnvId = requestedEnvId;
	}

	function openCreateSheet() {
		snippetToEdit = null;
		isFormSheetOpen = true;
	}

	function openEditSheet(snippet: Snippet) {
		snippetToEdit = snippet;
		isFormSheetOpen = true;
	}

	function openRunSheet(snippet: Snippet) {
		snippetToRun = snippet;
		isRunSheetOpen = true;
	}

	async function handleFormSubmit(payload: SnippetFormPayload) {
		isSubmitting = true;
		try {
			if (payload.mode === 'edit') {
				await snippetService.updateSnippet(pageState.envId, payload.id, payload.snippet);
				toast.success(m.common_update_success({ resource: m.snippets_singular() }));
			} else {
				await snippetService.createSnippet(pageState.envId, payload.snippet);
				toast.success(m.common_create_success({ resource: m.snippets_singular() }));
			}
			await loadSnippets();
			isFormSheetOpen = false;
			snippetToEdit = null;
		} catch (error) {
			toast.error(
				payload.mode === 'edit'
					? m.common_update_failed({ resource: m.snippets_singular() })
					: m.common_create_failed({ resource: m.snippets_singular() })
			);
			console.error('Error saving snippet:', error);
		} finally {
			isSubmitting = false;
		}
	}

	function handleDelete(snippet: Snippet) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: m.snippets_singular() }),
			message: m.common_delete_confirm({ resource: snippet.name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					try {
						await snippetService.deleteSnippet(pageState.envId, snippet.id);
						toast.success(m.common_delete_success({ resource: m.snippets_singular() }));
						if (snippetToEdit?.id === snippet.id) {
							isFormSheetOpen = false;
							snippetToEdit = null;
						}
						await loadSnippets();
					} catch (error) {
						toast.error(m.common_delete_failed({ resource: m.snippets_singular() }));
						console.error('Error deleting snippet:', error);
					}
				}
			}
		});
	}

	const canCreate = $derived(hasPermission('snippets:create', pageState.envId));

	const actionButtons = $derived<ActionButton[]>(
		canCreate
			? [
					{
						id: 'create',
						action: 'create',
						label: m.common_create_button({ resource: m.snippets_singular() }),
						onclick: openCreateSheet,
						disabled: !resourcesReady
					}
				]
			: []
	);
</script>

<ResourcePageLayout title={m.snippets_title()} subtitle={m.snippets_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<SnippetTable
				bind:snippets={pageState.items}
				bind:requestOptions={pageState.requestOptions}
				environmentId={displayedEnvId!}
				onRefreshData={loadSnippets}
				onRun={openRunSheet}
				onEdit={openEditSheet}
			/>
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<SnippetFormSheet
			bind:open={isFormSheetOpen}
			bind:snippetToEdit
			environmentId={pageState.envId}
			isLoading={isSubmitting}
			onSubmit={handleFormSubmit}
			onDelete={handleDelete}
		/>
		<SnippetRunSheet bind:open={isRunSheetOpen} snippet={snippetToRun} environmentId={pageState.envId} />
	{/snippet}
</ResourcePageLayout>

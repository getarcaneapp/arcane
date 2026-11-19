<script lang="ts">
	import { onDestroy, onMount, tick, untrack } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import FileTreeRow from '#lib/components/file-tree-row.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { createVirtualizer } from '#lib/components/ui/virtualizer.svelte.js';
	import VirtualRows from '#lib/components/virtual-rows.svelte';
	import * as m from '#lib/paraglide/messages.js';
	import type { BackupFileEntry } from '#lib/types/backup.js';
	import type { BackupFileProvider, BackupFileRootLoadState } from '#lib/types/ui/backup.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type FolderPageState = {
		entries: BackupFileEntry[];
		continuationStart?: number;
		loading: boolean;
		error: string | null;
		requestID: number;
	};

	type PickerRow =
		| { kind: 'entry'; entry: BackupFileEntry; depth: number }
		| { kind: 'continuation'; folder: string; depth: number };

	let {
		provider,
		selectedPaths = $bindable([]),
		selectAll = $bindable(false),
		search = $bindable(''),
		onRootLoad
	}: {
		provider: BackupFileProvider;
		selectedPaths?: string[];
		selectAll?: boolean;
		search?: string;
		onRootLoad?: (state: BackupFileRootLoadState) => void;
	} = $props();

	let pages = $state<Record<string, FolderPageState>>({});
	const expandedFolders = new SvelteSet<string>();
	const selectedDirectoryPaths = new SvelteSet<string>();
	let debouncedSearch = $state(untrack(() => search.trim()));
	let generation = 0;
	let searchTimeout: ReturnType<typeof setTimeout> | undefined;
	let nextRequestID = 0;
	let scrollElement = $state<HTMLElement | null>(null);

	const selectedSet = $derived(new Set(selectedPaths));
	const searchActive = $derived(debouncedSearch.length > 0);
	const rows = $derived.by<PickerRow[]>(() => {
		const result: PickerRow[] = [];
		appendFolderRows(result, '', 0);
		return result;
	});
	const rootLoading = $derived(pages['']?.loading === true && pages['']?.entries.length === 0);
	const rootError = $derived(
		pages[''] !== undefined && !pages['']?.loading && pages['']?.entries.length === 0 ? (pages['']?.error ?? null) : null
	);
	const rootEmpty = $derived(
		pages[''] !== undefined && !pages['']?.loading && !pages['']?.error && pages['']?.entries.length === 0
	);

	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLDivElement>(() => ({
		count: rows.length,
		getScrollElement: () => scrollElement,
		getItemKey: (index) => {
			const row = rows[index];
			return row?.kind === 'entry' ? `entry:${row.entry.path}` : `continuation:${row?.folder ?? ''}`;
		},
		estimateSize: () => 34,
		overscan: 8,
		onChange: (instance) => {
			loadVisibleContinuations(instance.getVirtualItems().map((item) => item.index));
		}
	}));

	function pageState(folder: string): FolderPageState {
		return pages[folder] ?? { entries: [], loading: false, error: null, requestID: 0 };
	}

	function updatePage(folder: string, state: FolderPageState) {
		pages = { ...pages, [folder]: state };
	}

	function appendFolderRows(result: PickerRow[], folder: string, depth: number) {
		const state = pages[folder];
		if (!state) return;
		for (const entry of state.entries) {
			result.push({ kind: 'entry', entry, depth });
			if (!searchActive && entry.isDirectory && expandedFolders.has(entry.path)) {
				appendFolderRows(result, entry.path, depth + 1);
			}
		}
		const initialNestedFolderLoad = folder !== '' && state.loading && state.entries.length === 0;
		if (!initialNestedFolderLoad && (state.loading || state.error || state.continuationStart !== undefined)) {
			result.push({ kind: 'continuation', folder, depth });
		}
	}

	function folderLoading(folder: string): boolean {
		const state = pages[folder];
		return state?.loading === true && state.entries.length === 0;
	}

	function resetPages() {
		generation += 1;
		pages = {};
		expandedFolders.clear();
		void loadPage('', 0, true, generation);
	}

	async function loadPage(folder: string, start: number, replace: boolean, requestGeneration = generation) {
		const current = pageState(folder);
		if (current.loading) return;
		const requestID = ++nextRequestID;
		const rootLoad = folder === '' && replace;
		updatePage(folder, { ...current, loading: true, error: null, requestID });
		if (rootLoad) onRootLoad?.('loading');

		const page = await tryCatch(
			provider.browse({
				path: searchActive ? undefined : folder,
				search: searchActive ? debouncedSearch : undefined,
				start: start > 0 ? start : undefined
			})
		);
		const latest = pages[folder];
		if (page.error !== null) {
			if (generation !== requestGeneration || latest?.requestID !== requestID) return;
			updatePage(folder, { ...latest, loading: false, error: extractApiErrorMessage(page.error) });
			if (rootLoad) onRootLoad?.('error');
			return;
		}
		if (generation === requestGeneration && latest?.requestID === requestID) {
			const merged = replace ? page.data.data : [...latest.entries, ...page.data.data];
			const unique = Array.from(new Map(merged.map((entry) => [entry.path, entry])).values());
			const continuationStart = start + page.data.data.length;
			updatePage(folder, {
				entries: unique,
				continuationStart: continuationStart < page.data.pagination.totalItems ? continuationStart : undefined,
				loading: false,
				error: null,
				requestID
			});
			if (rootLoad) onRootLoad?.('ready');
		}
		await tick();
		if (generation === requestGeneration) {
			loadVisibleContinuations(rowVirtualizer.virtualItems.map((item) => item.index));
		}
	}

	async function toggleFolder(folder: string) {
		if (searchActive) return;
		const requestGeneration = generation;
		if (expandedFolders.has(folder)) {
			expandedFolders.delete(folder);
		} else {
			expandedFolders.add(folder);
			if (!pages[folder]) void loadPage(folder, 0, true);
		}
		await tick();
		if (generation === requestGeneration) {
			loadVisibleContinuations(rowVirtualizer.virtualItems.map((item) => item.index));
		}
	}

	function coveringAncestor(entryPath: string): string | undefined {
		for (const candidate of selectedSet) {
			if (selectedDirectoryPaths.has(candidate) && entryPath.startsWith(candidate + '/')) return candidate;
		}
		return undefined;
	}

	function entryChecked(entry: BackupFileEntry): boolean {
		return selectAll || selectedSet.has(entry.path) || coveringAncestor(entry.path) !== undefined;
	}

	function entryIndeterminate(entry: BackupFileEntry): boolean {
		if (!entry.isDirectory || entryChecked(entry)) return false;
		return selectedPaths.some((candidate) => candidate.startsWith(entry.path + '/'));
	}

	function toggleEntry(entry: BackupFileEntry, checked: boolean) {
		if (selectAll || coveringAncestor(entry.path)) return;
		if (!checked) {
			selectedPaths = [...new Set(selectedPaths)].filter((candidate) => candidate !== entry.path);
			return;
		}
		if (entry.isDirectory) selectedDirectoryPaths.add(entry.path);
		selectedPaths = [
			...new Set([
				...selectedPaths.filter((candidate) => !(entry.isDirectory && candidate.startsWith(entry.path + '/'))),
				entry.path
			])
		];
	}

	function selectAllEntries() {
		selectedPaths = [];
		selectAll = true;
	}

	function clearSelection() {
		selectedPaths = [];
		selectAll = false;
	}

	onMount(() => {
		void loadPage('', 0, true);
	});
	onDestroy(() => {
		generation += 1;
		clearTimeout(searchTimeout);
	});

	function updateSearch(value: string) {
		const query = value.trim();
		if (selectAll && query !== search.trim()) selectAll = false;
		search = value;
		clearTimeout(searchTimeout);
		if (query === debouncedSearch) return;
		searchTimeout = setTimeout(() => {
			debouncedSearch = query;
			resetPages();
		}, 250);
	}

	function loadVisibleContinuations(indices: number[]) {
		for (const index of indices) {
			const row = rows[index];
			if (row?.kind !== 'continuation') continue;
			const state = pages[row.folder];
			if (state?.continuationStart !== undefined && !state.loading && !state.error) {
				void loadPage(row.folder, state.continuationStart, false);
			}
		}
	}
</script>

{#snippet pickerRow(row: PickerRow)}
	{#if row.kind === 'entry'}
		<FileTreeRow
			name={searchActive ? row.entry.path : row.entry.name}
			path={row.entry.path}
			depth={searchActive ? 0 : row.depth}
			isDirectory={row.entry.isDirectory}
			expanded={expandedFolders.has(row.entry.path)}
			showDisclosure={!searchActive}
			selectable
			checked={entryChecked(row.entry)}
			indeterminate={entryIndeterminate(row.entry)}
			disabled={selectAll || coveringAncestor(row.entry.path) !== undefined}
			loading={row.entry.isDirectory && expandedFolders.has(row.entry.path) && folderLoading(row.entry.path)}
			expandLabel={m.workspace_file_expand_folder({ name: row.entry.name })}
			collapseLabel={m.workspace_file_collapse_folder({ name: row.entry.name })}
			onToggle={() => toggleFolder(row.entry.path)}
			onActivate={() => (row.entry.isDirectory ? toggleFolder(row.entry.path) : toggleEntry(row.entry, !entryChecked(row.entry)))}
			onCheckedChange={(checked) => toggleEntry(row.entry, checked)}
		/>
	{:else}
		{@const state = pages[row.folder]}
		<div
			class="flex min-h-8 items-center gap-1.5 pr-2 pl-(--indent) text-xs text-muted-foreground"
			style={`--indent: ${0.5 + row.depth * 1}rem`}
		>
			{#if state?.error}
				<span>{m.backup_file_browser_load_remaining_failed()}</span>
				<span class="truncate" title={state.error}>{state.error}</span>
				<ArcaneButton
					action="base"
					tone="ghost"
					customLabel={m.common_retry()}
					onclick={() => loadPage(row.folder, state.continuationStart ?? 0, state.entries.length === 0)}
				/>
			{:else}
				{#if !searchActive}
					<span class="inline-flex size-4 shrink-0"></span>
				{/if}
				<span class="inline-flex size-4 shrink-0 items-center justify-center">
					<Spinner class="size-4" />
				</span>
			{/if}
		</div>
	{/if}
{/snippet}

<div class="space-y-3">
	<div class="flex items-center justify-between gap-2">
		<Input class="h-9" placeholder={m.volume_search_files()} bind:value={() => search, updateSearch} />
		<div class="flex items-center gap-2">
			<ArcaneButton
				action="base"
				tone="ghost"
				onclick={selectAllEntries}
				customLabel={search.trim() ? m.backup_file_browser_select_all_matches() : m.common_select_all()}
			/>
			<ArcaneButton action="base" tone="ghost" onclick={clearSelection} customLabel={m.common_clear()} />
		</div>
	</div>

	<div bind:this={scrollElement} class="h-64 overflow-auto rounded-md border p-1" data-backup-file-tree>
		{#if rootLoading}
			<div class="flex items-center justify-center py-8">
				<Spinner tone="muted" class="size-5" />
			</div>
		{:else if rootError !== null}
			<div class="flex flex-col items-center justify-center gap-2 px-4 py-8 text-center text-sm" data-backup-file-root-error>
				<span class="font-medium">{m.backup_file_browser_load_failed()}</span>
				<span class="text-xs text-muted-foreground">{rootError}</span>
				<ArcaneButton action="base" tone="ghost" customLabel={m.common_retry()} onclick={() => loadPage('', 0, true)} />
			</div>
		{:else if rootEmpty}
			<div class="flex items-center justify-center py-8 text-sm text-muted-foreground">
				{m.volume_backup_no_files()}
			</div>
		{:else}
			<VirtualRows virtualizer={rowVirtualizer} {rows} class="min-w-max" row={pickerRow} />
		{/if}
	</div>
</div>

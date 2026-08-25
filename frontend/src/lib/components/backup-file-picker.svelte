<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button';
	import FileTreeRow from '#lib/components/file-tree-row.svelte';
	import { Input } from '#lib/components/ui/input';
	import { Spinner } from '#lib/components/ui/spinner';
	import { createVirtualizer } from '#lib/components/ui/virtualizer.svelte';
	import type { BackupFileEntry, BackupFileProvider } from '#lib/types/backup';
	import * as m from '#lib/paraglide/messages.js';

	type FolderPageState = {
		entries: BackupFileEntry[];
		continuationStart?: number;
		loading: boolean;
		error: boolean;
		requestID: number;
	};

	type PickerRow =
		| { kind: 'entry'; entry: BackupFileEntry; depth: number }
		| { kind: 'continuation'; folder: string; depth: number };

	let {
		provider,
		selectedPaths = $bindable([]),
		selectAll = $bindable(false),
		search = $bindable('')
	}: {
		provider: BackupFileProvider;
		selectedPaths?: string[];
		selectAll?: boolean;
		search?: string;
	} = $props();

	let pages = $state<Record<string, FolderPageState>>({});
	let expandedFolders = $state<Set<string>>(new Set());
	let selectedDirectoryPaths = $state<Set<string>>(new Set());
	let debouncedSearch = $state('');
	let selectionSearch = $state('');
	let generation = $state(0);
	let nextRequestID = 0;
	let initializedProvider: BackupFileProvider | null = null;
	let scrollElement = $state<HTMLElement | null>(null);

	const selectedSet = $derived(new Set(selectedPaths));
	const searchActive = $derived(debouncedSearch.length > 0);
	const rows = $derived.by<PickerRow[]>(() => {
		const result: PickerRow[] = [];
		appendFolderRowsInternal(result, '', 0);
		return result;
	});
	const rootLoading = $derived(pages['']?.loading === true && pages['']?.entries.length === 0);
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
		overscan: 8
	}));

	function pageStateInternal(folder: string): FolderPageState {
		return pages[folder] ?? { entries: [], loading: false, error: false, requestID: 0 };
	}

	function updatePageInternal(folder: string, state: FolderPageState) {
		pages = { ...pages, [folder]: state };
	}

	function appendFolderRowsInternal(result: PickerRow[], folder: string, depth: number) {
		const state = pages[folder];
		if (!state) return;
		for (const entry of state.entries) {
			result.push({ kind: 'entry', entry, depth });
			if (!searchActive && entry.isDirectory && expandedFolders.has(entry.path)) {
				appendFolderRowsInternal(result, entry.path, depth + 1);
			}
		}
		const initialNestedFolderLoad = folder !== '' && state.loading && state.entries.length === 0;
		if (!initialNestedFolderLoad && (state.loading || state.error || state.continuationStart !== undefined)) {
			result.push({ kind: 'continuation', folder, depth });
		}
	}

	function folderLoadingInternal(folder: string): boolean {
		const state = pages[folder];
		return state?.loading === true && state.entries.length === 0;
	}

	function resetPagesInternal() {
		generation += 1;
		pages = {};
		expandedFolders = new Set();
		void loadPageInternal('', 0, true, generation);
	}

	async function loadPageInternal(folder: string, start: number, replace: boolean, requestGeneration = generation) {
		const current = pageStateInternal(folder);
		if (current.loading) return;
		const requestID = ++nextRequestID;
		updatePageInternal(folder, { ...current, loading: true, error: false, requestID });
		try {
			const page = await provider.browse({
				path: searchActive ? undefined : folder,
				search: searchActive ? debouncedSearch : undefined,
				start: start > 0 ? start : undefined
			});
			const latest = pages[folder];
			if (generation !== requestGeneration || latest?.requestID !== requestID) return;
			const merged = replace ? page.data : [...latest.entries, ...page.data];
			const unique = Array.from(new Map(merged.map((entry) => [entry.path, entry])).values());
			const continuationStart = start + page.data.length;
			updatePageInternal(folder, {
				entries: unique,
				continuationStart: continuationStart < page.pagination.totalItems ? continuationStart : undefined,
				loading: false,
				error: false,
				requestID
			});
		} catch {
			const latest = pages[folder];
			if (generation !== requestGeneration || latest?.requestID !== requestID) return;
			updatePageInternal(folder, { ...latest, loading: false, error: true });
		}
	}

	function toggleFolderInternal(folder: string) {
		if (searchActive) return;
		const next = new Set(expandedFolders);
		if (next.has(folder)) {
			next.delete(folder);
			expandedFolders = next;
			return;
		}
		next.add(folder);
		expandedFolders = next;
		if (!pages[folder]) void loadPageInternal(folder, 0, true);
	}

	function coveringAncestorInternal(entryPath: string): string | undefined {
		for (const candidate of selectedSet) {
			if (selectedDirectoryPaths.has(candidate) && entryPath.startsWith(candidate + '/')) return candidate;
		}
		return undefined;
	}

	function entryCheckedInternal(entry: BackupFileEntry): boolean {
		return selectAll || selectedSet.has(entry.path) || coveringAncestorInternal(entry.path) !== undefined;
	}

	function entryIndeterminateInternal(entry: BackupFileEntry): boolean {
		if (!entry.isDirectory || entryCheckedInternal(entry)) return false;
		return selectedPaths.some((candidate) => candidate.startsWith(entry.path + '/'));
	}

	function toggleEntryInternal(entry: BackupFileEntry, checked: boolean) {
		if (selectAll || coveringAncestorInternal(entry.path)) return;
		const next = new Set(selectedPaths);
		if (!checked) {
			next.delete(entry.path);
			selectedPaths = [...next];
			return;
		}
		if (entry.isDirectory) {
			for (const candidate of next) {
				if (candidate.startsWith(entry.path + '/')) next.delete(candidate);
			}
			const directories = new Set(selectedDirectoryPaths);
			directories.add(entry.path);
			selectedDirectoryPaths = directories;
		}
		next.add(entry.path);
		selectedPaths = [...next];
	}

	function selectAllInternal() {
		selectedPaths = [];
		selectAll = true;
		selectionSearch = search.trim();
	}

	function clearSelectionInternal() {
		selectedPaths = [];
		selectAll = false;
		selectionSearch = '';
	}

	$effect(() => {
		const currentProvider = provider;
		if (initializedProvider === currentProvider) return;
		initializedProvider = currentProvider;
		debouncedSearch = search.trim();
		resetPagesInternal();
	});

	$effect(() => {
		const query = search.trim();
		if (selectAll && query !== selectionSearch) {
			selectAll = false;
			selectionSearch = '';
		}
		if (query === debouncedSearch) return;
		const timeout = window.setTimeout(() => {
			debouncedSearch = query;
			resetPagesInternal();
		}, 250);
		return () => window.clearTimeout(timeout);
	});

	$effect(() => {
		for (const virtualItem of rowVirtualizer.virtualItems) {
			const row = rows[virtualItem.index];
			if (row?.kind !== 'continuation') continue;
			const state = pages[row.folder];
			if (state?.continuationStart !== undefined && !state.loading && !state.error) {
				void loadPageInternal(row.folder, state.continuationStart, false);
			}
		}
	});
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between gap-2">
		<Input class="h-9" placeholder={m.volume_search_files()} bind:value={search} />
		<div class="flex items-center gap-2">
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				onclick={selectAllInternal}
				customLabel={search.trim() ? m.backup_file_browser_select_all_matches() : m.common_select_all()}
			/>
			<ArcaneButton action="base" tone="ghost" size="sm" onclick={clearSelectionInternal} customLabel={m.common_clear()} />
		</div>
	</div>

	<div bind:this={scrollElement} class="h-64 overflow-auto rounded-md border p-1" data-backup-file-tree>
		{#if rootLoading}
			<div class="flex items-center justify-center py-8">
				<Spinner class="size-5 text-muted-foreground" />
			</div>
		{:else if rootEmpty}
			<div class="flex items-center justify-center py-8 text-sm text-muted-foreground">
				{m.volume_backup_no_files()}
			</div>
		{:else}
			<div class="relative min-w-max" style={`height: ${rowVirtualizer.totalSize}px`}>
				{#each rowVirtualizer.virtualItems as virtualItem (virtualItem.key)}
					{@const row = rows[virtualItem.index]}
					{#if row}
						<div
							class="absolute top-0 left-0 w-full"
							style={`transform: translateY(${virtualItem.start}px)`}
							data-index={virtualItem.index}
							use:rowVirtualizer.measureElement
						>
							{#if row.kind === 'entry'}
								<FileTreeRow
									name={searchActive ? row.entry.path : row.entry.name}
									path={row.entry.path}
									depth={searchActive ? 0 : row.depth}
									isDirectory={row.entry.isDirectory}
									expanded={expandedFolders.has(row.entry.path)}
									showDisclosure={!searchActive}
									selectable
									checked={entryCheckedInternal(row.entry)}
									indeterminate={entryIndeterminateInternal(row.entry)}
									disabled={selectAll || coveringAncestorInternal(row.entry.path) !== undefined}
									loading={row.entry.isDirectory && expandedFolders.has(row.entry.path) && folderLoadingInternal(row.entry.path)}
									expandLabel={m.workspace_file_expand_folder({ name: row.entry.name })}
									collapseLabel={m.workspace_file_collapse_folder({ name: row.entry.name })}
									onToggle={() => toggleFolderInternal(row.entry.path)}
									onActivate={() =>
										row.entry.isDirectory
											? toggleFolderInternal(row.entry.path)
											: toggleEntryInternal(row.entry, !entryCheckedInternal(row.entry))}
									onCheckedChange={(checked) => toggleEntryInternal(row.entry, checked)}
								/>
							{:else}
								{@const state = pages[row.folder]}
								<div
									class="flex min-h-8 items-center gap-1.5 pr-2 text-xs text-muted-foreground"
									style={`padding-left: ${0.5 + row.depth * 1}rem`}
								>
									{#if state?.error}
										<span>{m.backup_file_browser_load_remaining_failed()}</span>
										<ArcaneButton
											action="base"
											tone="ghost"
											size="sm"
											customLabel={m.common_retry()}
											onclick={() => loadPageInternal(row.folder, state.continuationStart ?? 0, state.entries.length === 0)}
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
						</div>
					{/if}
				{/each}
			</div>
		{/if}
	</div>
</div>

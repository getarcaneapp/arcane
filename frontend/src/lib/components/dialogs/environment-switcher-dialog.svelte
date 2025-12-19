<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import type { Environment } from '$lib/types/environment.type';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import settingsStore from '$lib/stores/config-store';
	import { debounced } from '$lib/utils/utils';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import { tick } from 'svelte';
	import { EnvironmentsIcon, RemoteEnvironmentIcon, AddIcon, SearchIcon, CloseIcon } from '$lib/icons';

	type Props = {
		open: boolean;
		isAdmin?: boolean;
	};

	let { open = $bindable(false), isAdmin = false }: Props = $props();

	let searchQuery = $state('');
	let environments = $state<Environment[]>([]);
	let isLoading = $state(false);
	let isLoadingMore = $state(false);
	let currentPage = $state(1);
	let totalPages = $state(1);
	let scrollContainer = $state<HTMLDivElement | null>(null);
	let lastScrollTop = $state(0);
	let loadError = $state<string | null>(null);
	let environmentsPromise = $state<Promise<void> | null>(null);
	let currentRequestId = 0;

	const PAGE_SIZE = 10;

	const DEFAULT_REQUEST_OPTIONS: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: PAGE_SIZE },
		sort: { column: 'name', direction: 'asc' },
		search: undefined
	};

	let requestOptions = $state<SearchPaginationSortRequest>(DEFAULT_REQUEST_OPTIONS);

	async function resetScrollToTop() {
		await tick();
		if (scrollContainer) scrollContainer.scrollTop = 0;
		lastScrollTop = 0;
	}

	function normalizeSearch(query: string): string | undefined {
		const trimmed = query.trim();
		return trimmed ? trimmed : undefined;
	}

	async function fetchEnvironments(options: SearchPaginationSortRequest, append: boolean, throwOnError = false) {
		currentRequestId++;
		const requestId = currentRequestId;
		loadError = null;
		requestOptions = options;

		if (append) {
			isLoadingMore = true;
		} else {
			isLoading = true;
			isLoadingMore = false;
		}

		try {
			const result = await environmentManagementService.getEnvironments(options);
			if (requestId !== currentRequestId) return;

			environments = append ? [...environments, ...result.data] : result.data;
			currentPage = result.pagination.currentPage;
			totalPages = result.pagination.totalPages;
		} catch (error) {
			if (requestId !== currentRequestId) return;
			console.error('Failed to load environments:', error);
			loadError = 'Failed to load environments';
			toast.error(loadError);
			if (throwOnError) throw error;
		} finally {
			if (requestId !== currentRequestId) return;
			isLoading = false;
			isLoadingMore = false;
		}
	}

	async function loadInitial() {
		environments = [];
		currentPage = 1;
		totalPages = 1;
		await resetScrollToTop();
		const options: SearchPaginationSortRequest = {
			...requestOptions,
			search: normalizeSearch(searchQuery),
			pagination: { page: 1, limit: PAGE_SIZE },
			sort: { column: 'name', direction: 'asc' }
		};
		await fetchEnvironments(options, false, true);
	}

	function resetDialogState() {
		// Invalidate any inflight request
		currentRequestId++;
		searchQuery = '';
		requestOptions = DEFAULT_REQUEST_OPTIONS;
		lastScrollTop = 0;
		environmentsPromise = null;
		loadError = null;
		// Keep environments around or clear? Clear so reopening always shows a clean slate
		environments = [];
		currentPage = 1;
		totalPages = 1;
	}

	function startInitialLoad() {
		environmentsPromise = Promise.resolve().then(() => loadInitial());
	}

	function closeDialog() {
		open = false;
		resetDialogState();
	}

	function openSession(node: HTMLElement) {
		// Runs when the dialog content mounts (i.e., when `open` becomes true)
		startInitialLoad();
		return {
			destroy() {
				// Runs when the dialog content unmounts (i.e., when `open` becomes false)
				resetDialogState();
			}
		};
	}

	const debouncedSearch = debounced((query: string) => {
		// Prevent stale debounced callbacks from re-applying an old query (e.g. after clearing the input)
		if (query !== searchQuery) return;
		const options: SearchPaginationSortRequest = {
			...requestOptions,
			search: normalizeSearch(query),
			pagination: { page: 1, limit: PAGE_SIZE },
			sort: { column: 'name', direction: 'asc' }
		};
		environmentsPromise = Promise.resolve().then(async () => {
			if (query !== searchQuery) return;
			await resetScrollToTop();
			await fetchEnvironments(options, false, true);
		});
	}, 300);

	function clearSearch() {
		searchQuery = '';
		const options: SearchPaginationSortRequest = {
			...requestOptions,
			search: undefined,
			pagination: { page: 1, limit: PAGE_SIZE },
			sort: { column: 'name', direction: 'asc' }
		};
		environmentsPromise = Promise.resolve().then(async () => {
			await resetScrollToTop();
			await fetchEnvironments(options, false, true);
		});
	}

	function handleScroll(e: Event) {
		const target = e.target as HTMLDivElement;
		const { scrollTop, scrollHeight, clientHeight } = target;

		// If content isn't scrollable yet, don't auto-fetch more pages.
		if (scrollHeight <= clientHeight) return;

		// Only react to downward scrolling; prevents programmatic scrollTop resets from triggering load-more loops.
		if (scrollTop <= lastScrollTop) return;
		lastScrollTop = scrollTop;

		// Load more when user scrolls near the bottom (within 50px)
		if (scrollHeight - scrollTop - clientHeight < 50) {
			if (!isLoadingMore && currentPage < totalPages) {
				loadMoreEnvironments();
			}
		}
	}

	async function loadMoreEnvironments() {
		if (isLoading || isLoadingMore) return;
		if (currentPage >= totalPages) return;
		isLoadingMore = true;
		try {
			const options: SearchPaginationSortRequest = {
				...requestOptions,
				search: normalizeSearch(searchQuery),
				pagination: { page: currentPage + 1, limit: PAGE_SIZE },
				sort: { column: 'name', direction: 'asc' }
			};
			await fetchEnvironments(options, true, false);
		} catch {
			// fetchEnvironments already handles toasts/errors (and stale-response protection)
		} finally {
			// fetchEnvironments controls isLoadingMore; this is only a safety net.
			isLoadingMore = false;
		}
	}

	async function handleSelect(env: Environment) {
		if (!env || !env.enabled) return;
		try {
			await environmentStore.setEnvironment(env);
			closeDialog();
			toast.success(m.environments_switched_to({ name: env.name }));
		} catch (error) {
			console.error('Failed to set environment:', error);
			toast.error('Failed to Connect to Environment');
		}
	}

	function getConnectionString(env: Environment): string {
		if (env.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		} else {
			return env.apiUrl;
		}
	}
</script>

<ResponsiveDialog bind:open title={m.sidebar_select_environment()} contentClass="max-w-2xl">
	{#snippet children()}
		<div class="m-2 flex flex-col gap-4">
			{#if open}
				<div class="hidden" use:openSession aria-hidden="true"></div>
			{/if}
			<div class="relative">
				<SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
				<Input
					type="text"
					placeholder={m.common_search()}
					value={searchQuery}
					oninput={(e) => {
						searchQuery = (e.target as HTMLInputElement).value;
						debouncedSearch(searchQuery);
					}}
					class="h-9 pr-10 pl-10"
				/>
				{#if searchQuery}
					<button
						type="button"
						onclick={clearSearch}
						class="text-muted-foreground hover:text-foreground hover:bg-muted absolute top-1/2 right-3 -translate-y-1/2 rounded-sm p-0.5 transition-colors"
						title="Clear search"
					>
						<CloseIcon class="size-4" />
					</button>
				{/if}
			</div>

			<div bind:this={scrollContainer} onscroll={handleScroll} class="max-h-[50vh] min-h-[200px] overflow-y-auto">
				{#await environmentsPromise}
					<div class="flex items-center justify-center py-10">
						<Spinner class="size-6" />
					</div>
				{:then}
					{#if environments.length === 0}
						<div class="text-muted-foreground py-10 text-center">
							<EnvironmentsIcon class="mx-auto mb-4 size-12 opacity-50" />
							<p>{m.sidebar_no_environments()}</p>
						</div>
					{:else}
						<div class="space-y-1">
							{#each environments as env (env.id)}
								{@const isActive = environmentStore.selected?.id === env.id}
								{@const isDisabled = !env.enabled}
								<button
									type="button"
									onclick={() => !isActive && !isDisabled && handleSelect(env)}
									disabled={isDisabled}
									class={cn(
										'flex w-full items-center gap-3 rounded-lg p-3 text-left transition-colors',
										isActive && 'bg-primary/10 border-primary border font-medium',
										!isActive && !isDisabled && 'hover:bg-muted/50',
										isDisabled && 'cursor-not-allowed opacity-50'
									)}
								>
									<div
										class={cn(
											'flex size-8 shrink-0 items-center justify-center rounded-md border',
											isActive ? 'bg-primary border-primary' : 'border-border'
										)}
									>
										{#if env.id === '0'}
											<EnvironmentsIcon class={cn('size-4', isActive && 'text-primary-foreground')} />
										{:else}
											<RemoteEnvironmentIcon class={cn('size-4', isActive && 'text-primary-foreground')} />
										{/if}
									</div>
									<div class="flex min-w-0 flex-1 flex-col">
										<span class="truncate">{env.name}</span>
										<span class={cn('truncate text-xs', isActive ? 'text-primary/70' : 'text-muted-foreground')}>
											{getConnectionString(env)}
										</span>
									</div>
									{#if isActive}
										<span class="text-primary text-xs font-medium">{m.environments_current_environment()}</span>
									{/if}
								</button>
							{/each}

							{#if isLoadingMore}
								<div class="flex items-center justify-center py-4">
									<Spinner class="size-5" />
								</div>
							{/if}
						</div>
					{/if}
				{:catch}
					<div class="text-destructive py-10 text-center">
						<p>{m.error_generic()}</p>
					</div>
				{/await}
			</div>
		</div>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full items-center justify-between gap-2">
			{#if isAdmin}
				<ArcaneButton
					action="base"
					tone="outline"
					icon={AddIcon}
					customLabel={m.sidebar_manage_environments()}
					onclick={() => {
						closeDialog();
						goto('/environments');
					}}
				/>
			{:else}
				<div></div>
			{/if}
			<ArcaneButton action="base" tone="outline" customLabel={m.common_close()} onclick={closeDialog} />
		</div>
	{/snippet}
</ResponsiveDialog>

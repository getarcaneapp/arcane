<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
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
	import { EnvironmentsIcon, RemoteEnvironmentIcon, AddIcon, SearchIcon, CloseIcon } from '$lib/icons';

	type Props = {
		open: boolean;
		isAdmin?: boolean;
	};

	let { open = $bindable(false), isAdmin = false }: Props = $props();

	let searchQuery = $state('');
	let environments = $state<Environment[]>([]);
	let isLoadingMore = $state(false);
	let currentPage = $state(1);
	let totalPages = $state(1);
	let scrollContainer = $state<HTMLDivElement | null>(null);

	const PAGE_SIZE = 10;

	// Reactive promise that loads environments based on searchQuery
	let environmentsPromise = $derived.by(() => {
		if (!open) return null;

		return (async () => {
			try {
				const result = await environmentManagementService.getEnvironments({
					pagination: { page: 1, limit: PAGE_SIZE },
					search: searchQuery || undefined,
					sort: { column: 'name', direction: 'asc' }
				});

				environments = result.data;
				currentPage = result.pagination.currentPage;
				totalPages = result.pagination.totalPages;
			} catch (error) {
				console.error('Failed to load environments:', error);
				toast.error('Failed to load environments');
				throw error;
			}
		})();
	});

	const debouncedSearch = debounced((query: string) => {
		searchQuery = query;
	}, 300);

	function handleScroll(e: Event) {
		const target = e.target as HTMLDivElement;
		const { scrollTop, scrollHeight, clientHeight } = target;

		// Load more when user scrolls near the bottom (within 50px)
		if (scrollHeight - scrollTop - clientHeight < 50) {
			if (!isLoadingMore && currentPage < totalPages) {
				loadMoreEnvironments();
			}
		}
	}

	async function loadMoreEnvironments() {
		isLoadingMore = true;
		try {
			const result = await environmentManagementService.getEnvironments({
				pagination: { page: currentPage + 1, limit: PAGE_SIZE },
				search: searchQuery || undefined,
				sort: { column: 'name', direction: 'asc' }
			});

			environments = [...environments, ...result.data];
			currentPage = result.pagination.currentPage;
			totalPages = result.pagination.totalPages;
		} catch (error) {
			console.error('Failed to load more environments:', error);
			toast.error('Failed to load more environments');
		} finally {
			isLoadingMore = false;
		}
	}

	async function handleSelect(env: Environment) {
		if (!env || !env.enabled) return;
		try {
			await environmentStore.setEnvironment(env);
			open = false;
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
			<div class="relative">
				<SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
				<Input
					type="text"
					placeholder={m.common_search()}
					value={searchQuery}
					oninput={(e) => debouncedSearch((e.target as HTMLInputElement).value)}
					class="h-9 pr-10 pl-10"
				/>
				{#if searchQuery}
					<button
						type="button"
						onclick={() => (searchQuery = '')}
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
				<Button
					variant="outline"
					onclick={() => {
						open = false;
						goto('/environments');
					}}
				>
					<AddIcon class="mr-1.5 size-4" />
					{m.sidebar_manage_environments()}
				</Button>
			{:else}
				<div></div>
			{/if}
			<Button variant="ghost" onclick={() => (open = false)}>
				{m.common_close()}
			</Button>
		</div>
	{/snippet}
</ResponsiveDialog>

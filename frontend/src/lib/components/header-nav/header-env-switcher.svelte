<script lang="ts">
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { queryKeys } from '$lib/query/query-keys';
	import type { Environment } from '$lib/types/environment';
	import type { SearchPaginationSortRequest } from '$lib/types/shared';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import settingsStore from '$lib/stores/config-store';
	import { debounced } from '$lib/utils/ws';
	import {
		EnvironmentsIcon,
		RemoteEnvironmentIcon,
		EdgeConnectionIcon,
		ArrowsUpDownIcon,
		AddIcon,
		SearchIcon,
		SettingsIcon
	} from '$lib/icons';

	let open = $state(false);
	let searchQuery = $state('');
	let environments = $state<Environment[]>([]);
	let isLoading = $state(false);
	let isLoadingMore = $state(false);
	let currentPage = $state(1);
	let totalPages = $state(1);
	let currentRequestId = 0;

	const PAGE_SIZE = 10;
	const queryClient = useQueryClient();

	function buildOptions(page: number): SearchPaginationSortRequest {
		const trimmed = searchQuery.trim();
		return {
			pagination: { page, limit: PAGE_SIZE },
			sort: { column: 'name', direction: 'asc' },
			search: trimmed ? trimmed : undefined
		};
	}

	async function fetchEnvironments(page: number, append: boolean) {
		currentRequestId++;
		const requestId = currentRequestId;
		if (append) {
			isLoadingMore = true;
		} else {
			isLoading = true;
		}
		try {
			const options = buildOptions(page);
			const result = await queryClient.fetchQuery({
				queryKey: queryKeys.environments.switcher(options),
				queryFn: () => environmentManagementService.getEnvironments(options),
				staleTime: 0
			});
			if (requestId !== currentRequestId) return;
			environments = append ? [...environments, ...result.data] : result.data;
			currentPage = result.pagination.currentPage;
			totalPages = result.pagination.totalPages;
		} catch (error) {
			if (requestId !== currentRequestId) return;
			console.error('Failed to load environments:', error);
			toast.error('Failed to load environments');
		} finally {
			if (requestId !== currentRequestId) return;
			isLoading = false;
			isLoadingMore = false;
		}
	}

	const debouncedSearch = debounced((query: string) => {
		if (query !== searchQuery) return;
		void fetchEnvironments(1, false);
	}, 300);

	function handleOpenChange(next: boolean) {
		open = next;
		if (next) {
			searchQuery = '';
			environments = [];
			void fetchEnvironments(1, false);
		} else {
			currentRequestId++;
		}
	}

	async function handleSelect(env: Environment) {
		if (!env.enabled || environmentStore.selected?.id === env.id) return;
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
			return $settingsStore?.dockerHost || 'unix:///var/run/docker.sock';
		}
		return env.apiUrl;
	}
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
	<Popover.Trigger
		title={environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
		class="hover:bg-sidebar-accent/60 data-[state=open]:bg-sidebar-accent/60 flex h-9 max-w-56 shrink-0 items-center gap-2 rounded-lg px-2 text-sm transition-colors"
	>
		<span class="bg-primary text-primary-foreground flex size-6 shrink-0 items-center justify-center rounded-md">
			{#if !environmentStore.selected || environmentStore.selected.id === '0'}
				<EnvironmentsIcon class="size-3.5" />
			{:else if environmentStore.selected.isEdge}
				<EdgeConnectionIcon class="size-3.5" />
			{:else}
				<RemoteEnvironmentIcon class="size-3.5" />
			{/if}
		</span>
		<span class="hidden truncate font-medium lg:inline">
			{environmentStore.selected ? environmentStore.selected.name : m.sidebar_no_environment()}
		</span>
		<ArrowsUpDownIcon class="text-muted-foreground size-3.5 shrink-0" />
	</Popover.Trigger>
	<Popover.Content align="end" sideOffset={10} class="flex w-[min(94vw,380px)] flex-col gap-2 p-2">
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
				class="h-9 pl-10"
			/>
		</div>

		<div class="max-h-[min(50vh,360px)] min-h-24 overflow-y-auto">
			{#if isLoading}
				<div class="flex items-center justify-center py-8">
					<Spinner class="size-5" />
				</div>
			{:else if environments.length === 0}
				<div class="text-muted-foreground py-8 text-center text-sm">
					{m.sidebar_no_environments()}
				</div>
			{:else}
				<div class="space-y-1">
					{#each environments as env (env.id)}
						{@const isActive = environmentStore.selected?.id === env.id}
						{@const isDisabled = !env.enabled}
						<div
							class={cn(
								'group flex items-center gap-1 rounded-lg transition-colors',
								isActive && 'bg-primary/10 font-medium',
								!isActive && !isDisabled && 'hover:bg-muted/50',
								!isActive && isDisabled && 'opacity-50'
							)}
						>
							<button
								type="button"
								onclick={() => handleSelect(env)}
								disabled={isDisabled}
								class={cn(
									'flex min-w-0 flex-1 items-center gap-2.5 rounded-md p-2 text-left',
									isDisabled && 'cursor-not-allowed'
								)}
							>
								<span
									class={cn(
										'flex size-7 shrink-0 items-center justify-center rounded-md border',
										isActive ? 'bg-primary border-primary text-primary-foreground' : 'border-border'
									)}
								>
									{#if env.id === '0'}
										<EnvironmentsIcon class="size-3.5" />
									{:else if env.isEdge}
										<EdgeConnectionIcon class="size-3.5" />
									{:else}
										<RemoteEnvironmentIcon class="size-3.5" />
									{/if}
								</span>
								<span class="flex min-w-0 flex-1 flex-col">
									<span class="truncate text-sm">{env.name}</span>
									<span class={cn('truncate text-xs', isActive ? 'text-primary/70' : 'text-muted-foreground')}>
										{getConnectionString(env)}
									</span>
								</span>
							</button>
							<ArcaneButton
								action="base"
								tone="ghost"
								size="icon"
								class="mr-1 size-7 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
								icon={SettingsIcon}
								showLabel={false}
								customLabel={m.settings_title()}
								onclick={() => {
									open = false;
									goto(`/environments/${env.id}`);
								}}
							/>
						</div>
					{/each}

					{#if currentPage < totalPages}
						<button
							type="button"
							onclick={() => fetchEnvironments(currentPage + 1, true)}
							disabled={isLoadingMore}
							class="text-primary hover:bg-muted/50 flex w-full items-center justify-center gap-2 rounded-md p-2 text-xs font-medium"
						>
							{#if isLoadingMore}
								<Spinner class="size-4" />
							{:else}
								{m.common_load_more()}
							{/if}
						</button>
					{/if}
				</div>
			{/if}
		</div>

		<IfPermitted perm="environments:create">
			<div class="border-border/50 border-t pt-2">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					class="w-full justify-start"
					icon={AddIcon}
					customLabel={m.sidebar_manage_environments()}
					onclick={() => {
						open = false;
						goto('/environments');
					}}
				/>
			</div>
		</IfPermitted>
	</Popover.Content>
</Popover.Root>

<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import ActivityListItem from '$lib/components/activity/activity-list-item.svelte';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts';
	import {
		ActivityIcon,
		ApiKeyIcon,
		ArrowRightIcon,
		CheckIcon,
		ShieldAlertIcon,
		StopIcon,
		UpdateIcon,
		type IconType
	} from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
	import { operationsStore, type OperationsEnvironmentState } from '$lib/stores/operations.store.svelte';
	import type { Activity } from '$lib/types/activity.type';
	import type { OperationsState } from '$lib/types/operations';
	import { isEnvironmentOnline } from '$lib/utils/docker';

	let {}: PageProps = $props();

	type AttentionCategory = {
		key: 'updates' | 'stopped' | 'vulnerabilities' | 'keys';
		label: string;
		count: number;
		icon: IconType;
		href: string;
	};
	const environmentStates = $derived(
		Object.values(operationsStore.environmentStates).sort((a, b) =>
			a.id === LOCAL_DOCKER_ENVIRONMENT_ID ? -1 : b.id === LOCAL_DOCKER_ENVIRONMENT_ID ? 1 : a.name.localeCompare(b.name)
		)
	);
	const activities = $derived(activityStore.activities.slice(0, 3));
	const hasAttentionData = $derived(
		environmentStates.some((state) => state.hasLoaded && categoriesForState(state.operations).length > 0)
	);
	const attentionEnvironmentStates = $derived(
		environmentStates.filter(
			(state) => environmentUnavailable(state) || actionableCategoriesForState(state.operations).length > 0
		)
	);
	const allClear = $derived(
		hasAttentionData &&
			environmentStates.every(
				(state) =>
					state.hasLoaded && !environmentUnavailable(state) && actionableCategoriesForState(state.operations).length === 0
			)
	);

	onMount(() => {
		void operationsStore.start();
		void activityStore.start();
		return () => {
			operationsStore.stop();
		};
	});

	function categoriesForState(state: OperationsState | null): AttentionCategory[] {
		if (!state) return [];
		const categories: AttentionCategory[] = [];
		if (state.updates !== undefined) {
			categories.push({
				key: 'updates',
				label: m.operations_workload_updates(),
				count: state.updates.total,
				icon: UpdateIcon,
				href: '/operations/updates'
			});
		}
		if (state.stopped !== undefined) {
			categories.push({
				key: 'stopped',
				label: m.operations_stopped_workloads(),
				count: state.stopped.total,
				icon: StopIcon,
				href: '/workloads'
			});
		}
		if (state.vulnerabilities !== undefined) {
			categories.push({
				key: 'vulnerabilities',
				label: m.vuln_title(),
				count: state.vulnerabilities,
				icon: ShieldAlertIcon,
				href: '/images/vulnerabilities'
			});
		}
		if (state.expiringApiKeys !== undefined) {
			categories.push({
				key: 'keys',
				label: m.operations_expiring_keys(),
				count: state.expiringApiKeys,
				icon: ApiKeyIcon,
				href: '/settings/api-keys'
			});
		}
		return categories;
	}

	function actionableCategoriesForState(state: OperationsState | null): AttentionCategory[] {
		return categoriesForState(state).filter((category) => category.count > 0);
	}

	function environmentUnavailable(state: OperationsEnvironmentState): boolean {
		const environment = environmentStore.available.find((item) => item.id === state.id);
		return state.streamError || (!!environment && state.id !== LOCAL_DOCKER_ENVIRONMENT_ID && !isEnvironmentOnline(environment));
	}

	async function openCategory(state: OperationsEnvironmentState, category: AttentionCategory) {
		if (environmentUnavailable(state)) return;
		const environment = environmentStore.available.find((item) => item.id === state.id);
		if (environment && environmentStore.selected?.id !== environment.id) {
			await environmentStore.setEnvironment(environment);
		}
		await goto(category.href);
	}

	function openActivity(activity: Activity) {
		activityStore.openCenter(activity.id);
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: () => operationsStore.refresh()
		}
	]);
</script>

<ResourcePageLayout title={m.operations_title()} subtitle={m.operations_subtitle()} icon={ActivityIcon} {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-8 md:px-3">
			<section class="space-y-3">
				<header>
					<h2 class="text-lg font-semibold tracking-tight">{m.operations_needs_attention()}</h2>
					<p class="mt-0.5 text-sm text-muted-foreground">{m.operations_attention_description()}</p>
				</header>

				{#if allClear}
					<div class="rounded-xl border border-emerald-500/20 bg-emerald-500/5 px-4 py-4">
						<div class="flex items-center gap-3">
							<div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10">
								<CheckIcon class="size-4.5 text-emerald-500" />
							</div>
							<div>
								<p class="text-sm font-medium">{m.operations_all_clear()}</p>
								<p class="text-xs text-muted-foreground sm:text-sm">{m.operations_all_clear_description()}</p>
							</div>
						</div>
					</div>
				{:else if attentionEnvironmentStates.length > 0}
					<div class="divide-y divide-border/60 overflow-hidden rounded-xl border border-border/60 bg-background/60">
						{#each attentionEnvironmentStates as state (state.id)}
							{@const unavailable = environmentUnavailable(state)}
							{@const categories = actionableCategoriesForState(state.operations)}
							<section>
								<header class="flex items-center justify-between gap-4 bg-muted/20 px-4 py-3">
									<div class="min-w-0">
										<h3 class="truncate text-sm font-semibold">{state.name}</h3>
										<p class="mt-0.5 text-xs text-muted-foreground">
											{state.updatedAt
												? m.operations_last_updated({ timestamp: new Date(state.updatedAt).toLocaleString() })
												: m.common_loading()}
										</p>
									</div>
									<div class="flex shrink-0 gap-2">
										{#if state.operations?.compatibility === 'legacy'}
											<Badge variant="outline">{m.operations_legacy_summary()}</Badge>
										{/if}
										{#if unavailable}
											<Badge variant="destructive">
												{state.errorCode === 'agent_incompatible' ? m.operations_incompatible() : m.common_offline()}
											</Badge>
										{/if}
									</div>
								</header>
								<div class="divide-y divide-border/50 border-t border-border/50">
									{#each categories as category (category.key)}
										{@const Icon = category.icon}
										<button
											type="button"
											disabled={unavailable}
											onclick={() => openCategory(state, category)}
											class="group flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/30 disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:bg-transparent"
										>
											<div class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
												<Icon class="size-4" />
											</div>
											<p class="min-w-0 flex-1 truncate text-sm font-medium">{category.label}</p>
											<span class="text-base font-semibold tabular-nums">{category.count}</span>
											<ArrowRightIcon
												class="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5"
											/>
										</button>
									{/each}
									{#if categories.length === 0}
										<p class="px-4 py-3 text-sm text-muted-foreground">{m.operations_category_unavailable()}</p>
									{/if}
								</div>
							</section>
						{/each}
					</div>
				{:else}
					<div class="rounded-xl border border-border/60 px-4 py-6 text-center text-sm text-muted-foreground">
						{m.common_loading()}
					</div>
				{/if}
			</section>

			<section class="space-y-3">
				<header class="flex items-end justify-between gap-4">
					<div>
						<h2 class="text-lg font-semibold tracking-tight">{m.operations_recent_activity()}</h2>
						<p class="mt-0.5 text-sm text-muted-foreground">{m.operations_activity_description()}</p>
					</div>
					<button
						type="button"
						onclick={() => activityStore.openCenter()}
						class="shrink-0 text-sm font-medium text-primary transition-colors hover:text-primary/80"
					>
						{m.common_view_all()}
					</button>
				</header>
				<div class="divide-y divide-border/40 overflow-hidden rounded-xl border border-border/60 bg-background/60">
					{#each activities as activity (activity.id)}
						<button type="button" class="block w-full" onclick={() => openActivity(activity)}>
							<ActivityListItem {activity} compact />
						</button>
					{:else}
						<p class="py-8 text-center text-sm text-muted-foreground">{m.activity_empty_title()}</p>
					{/each}
				</div>
			</section>
		</div>
	{/snippet}
</ResourcePageLayout>

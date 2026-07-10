<script lang="ts">
	import { ActivityIcon } from '$lib/icons';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import userStore, { userHasPermissionInAnyEnvironment } from '$lib/stores/user-store';
	import { fromStore } from 'svelte/store';

	let {
		collapsed = false,
		mobile = false,
		compact = false,
		class: className = '',
		onOpen
	}: {
		collapsed?: boolean;
		mobile?: boolean;
		compact?: boolean;
		class?: string;
		onOpen?: () => void;
	} = $props();

	const activeCount = $derived(activityStore.activeCount);
	const isActive = $derived(activeCount > 0);
	const currentUser = fromStore(userStore);
	const canReadActivities = $derived(userHasPermissionInAnyEnvironment(currentUser.current, 'activities:read'));

	function handleOpenInternal() {
		activityStore.openCenter();
		onOpen?.();
	}
</script>

{#if canReadActivities}
	{#if compact}
		<button
			type="button"
			onclick={handleOpenInternal}
			title={m.activity_center_open()}
			aria-label={m.activity_center_open()}
			class={cn(
				'group focus-visible:ring-ring relative flex items-center transition-colors focus-visible:ring-2 focus-visible:outline-hidden',
				collapsed
					? 'mx-auto h-9 w-9 justify-center rounded-xl'
					: 'text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground h-8 w-full rounded-md px-2',
				className
			)}
		>
			<span
				class={cn(
					'grid place-items-center rounded-md transition-colors',
					collapsed ? 'size-8' : 'size-6',
					isActive
						? 'bg-primary/15 text-primary'
						: 'bg-sidebar-accent text-sidebar-foreground/60 group-hover:text-sidebar-foreground'
				)}
			>
				<ActivityIcon class={cn(collapsed ? 'size-4.5' : 'size-3.5')} aria-hidden="true" />
			</span>
			{#if !collapsed}
				<span class="ml-2 min-w-0 flex-1 truncate text-left text-xs font-medium">{m.activity_center_title()}</span>
				<span
					class={cn(
						'ml-2 inline-flex h-5 min-w-5 items-center justify-center rounded-full px-1.5 text-[10px] leading-none font-semibold tabular-nums',
						isActive ? 'bg-primary text-primary-foreground' : 'bg-sidebar-accent text-sidebar-foreground/45'
					)}
				>
					{isActive ? (activeCount > 9 ? m.activity_count_many() : activeCount) : '0'}
				</span>
			{:else if isActive}
				<span
					class="bg-primary text-primary-foreground ring-sidebar absolute -top-0.5 -right-0.5 flex min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-4 font-bold tabular-nums ring-2"
				>
					{activeCount > 9 ? m.activity_count_many() : activeCount}
				</span>
			{/if}
		</button>
	{:else}
		<button
			type="button"
			onclick={handleOpenInternal}
			title={m.activity_center_open()}
			aria-label={m.activity_center_open()}
			class={cn(
				'text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring relative flex items-center gap-3 rounded-lg transition-colors focus-visible:ring-2 focus-visible:outline-hidden',
				mobile
					? 'w-full px-4 py-3 text-sm font-medium'
					: collapsed
						? 'h-9 w-9 justify-center'
						: 'h-9 w-full px-3 text-sm font-medium',
				className
			)}
		>
			<span class="relative inline-flex">
				<ActivityIcon class={cn(mobile ? 'size-5' : 'size-4.5')} aria-hidden="true" />
				{#if activeCount > 0}
					<span
						aria-live="polite"
						aria-atomic="true"
						class="bg-primary text-primary-foreground absolute -top-2 -right-2 flex min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-4 font-bold tabular-nums"
					>
						{activeCount > 9 ? m.activity_count_many() : activeCount}
					</span>
				{/if}
			</span>
			{#if !collapsed || mobile}
				<span class="min-w-0 flex-1 truncate text-left">{m.activity_center_title()}</span>
				{#if activeCount > 0}
					<span class="text-primary text-xs font-semibold tabular-nums">{m.activity_active_count({ count: activeCount })}</span>
				{/if}
			{/if}
		</button>
	{/if}
{/if}

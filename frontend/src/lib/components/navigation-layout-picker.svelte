<script lang="ts">
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import userStore from '$lib/stores/user-store';
	import { navigationLayoutStore, type NavigationLayout } from '$lib/stores/navigation-layout.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { userService } from '$lib/services/user-service';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';

	const queryClient = useQueryClient();

	const updateLayoutMutation = createMutation(() => ({
		mutationFn: async (navigationLayout: NavigationLayout) => {
			if ($userStore) {
				await userService.updateMyProfile({ navigationLayout });
			}
			return navigationLayout;
		},
		onMutate: (navigationLayout) => {
			const previousLayout = navigationLayoutStore.current;
			navigationLayoutStore.current = navigationLayout;
			return { previousLayout };
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.users.all });
		},
		onError: (err, _layout, context) => {
			if (context) {
				navigationLayoutStore.current = context.previousLayout;
			}
			console.error('Failed to update navigation layout', err);
		}
	}));

	function selectLayout(layout: NavigationLayout) {
		if (layout === navigationLayoutStore.current) return;
		updateLayoutMutation.mutate(layout);
	}

	const options: { value: NavigationLayout; label: () => string }[] = [
		{ value: 'sidebar', label: () => m.navigation_layout_sidebar() },
		{ value: 'header', label: () => m.navigation_layout_header() }
	];
</script>

<div class="bg-muted/40 inline-flex rounded-lg p-0.5">
	{#each options as option (option.value)}
		<button
			type="button"
			onclick={() => selectLayout(option.value)}
			class={cn(
				'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
				navigationLayoutStore.current === option.value
					? 'bg-background text-foreground shadow-sm'
					: 'text-muted-foreground hover:text-foreground'
			)}
		>
			{option.label()}
		</button>
	{/each}
</div>

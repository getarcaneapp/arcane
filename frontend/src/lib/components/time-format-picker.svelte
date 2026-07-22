<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';
	import { queryKeys } from '$lib/query/query-keys';
	import { userService } from '$lib/services/user-service';
	import { timeFormatStore } from '$lib/stores/time-format.store.svelte';
	import userStore from '$lib/stores/user-store';
	import type { TimeFormat } from '$lib/types/auth';

	let {
		id = 'timeFormatPicker',
		class: className = ''
	}: {
		id?: string;
		class?: string;
	} = $props();

	const queryClient = useQueryClient();
	const options = $derived([
		{ value: 'auto' as const, label: m.auto() },
		{ value: '12h' as const, label: m.time_format_12_hour() },
		{ value: '24h' as const, label: m.time_format_24_hour() }
	]);
	const currentLabel = $derived(options.find((option) => option.value === timeFormatStore.current)?.label ?? m.auto());

	const updateTimeFormatMutation = createMutation(() => ({
		mutationFn: (timeFormat: TimeFormat) => userService.updateMyProfile({ timeFormat }),
		onMutate: (timeFormat) => {
			const previousTimeFormat = timeFormatStore.current;
			timeFormatStore.set(timeFormat);
			return { previousTimeFormat };
		},
		onSuccess: async (updatedUser) => {
			await userStore.setUser(updatedUser);
			await queryClient.invalidateQueries({ queryKey: queryKeys.users.all });
		},
		onError: (error, _timeFormat, context) => {
			timeFormatStore.set(context?.previousTimeFormat ?? 'auto');
			console.error('Failed to update time format', error);
		}
	}));
</script>

<div class={`time-format-picker ${className}`}>
	<Select.Root
		type="single"
		value={timeFormatStore.current}
		onValueChange={(value) => updateTimeFormatMutation.mutate(value as TimeFormat)}
	>
		<Select.Trigger {id} class="h-9 w-32 text-sm font-medium" aria-label={m.time_format_select()}>
			<span class="truncate">{currentLabel}</span>
		</Select.Trigger>
		<Select.Content class="max-w-70 min-w-40">
			{#each options as option (option.value)}
				<Select.Item class="text-sm" value={option.value}>{option.label}</Select.Item>
			{/each}
		</Select.Content>
	</Select.Root>
</div>

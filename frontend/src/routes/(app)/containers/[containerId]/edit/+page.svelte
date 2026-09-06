<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ContainerForm from '#lib/components/containers/container-form/container-form.svelte';
	import {
		containerFormSchema,
		formValuesFromEditConfig,
		rowsFromEditConfig,
		toEditRequest
	} from '#lib/components/containers/container-form/container-form-state.js';
	import { createForm } from '#lib/utils/settings.js';
	import { containerService } from '#lib/services/container-service.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { m } from '#lib/paraglide/messages.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { untrack } from 'svelte';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import type { ContainerEditRequest } from '#lib/types/docker.js';

	let { data } = $props();

	const queryClient = useQueryClient();
	// The form is intentionally seeded once from the load-time snapshot.
	// svelte-ignore state_referenced_locally
	const form = createForm<typeof containerFormSchema>(containerFormSchema, formValuesFromEditConfig(data.editConfig));
	let rows = $state(untrack(() => rowsFromEditConfig(data.editConfig)));

	const editContainerMutation = createMutation(() => ({
		mutationFn: (request: ContainerEditRequest) => containerService.editContainer(data.containerId, request),
		onSuccess: async (details) => {
			toast.success(m.edit_success(), activityToastOptions(extractActivityId(details)));
			await queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
			queryClient.removeQueries({ queryKey: queryKeys.containers.detail(data.envId, data.containerId) });
			queryClient.removeQueries({ queryKey: queryKeys.containers.editConfig(data.envId, data.containerId) });
			goto(`/containers/${details.id}`);
		},
		onError: (error) => {
			toast.error(m.edit_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	function handleSubmit() {
		const values = form.validate();
		if (!values) return;
		const request = toEditRequest(values, rows);
		openConfirmDialog({
			title: m.edit_confirm_title(),
			message: m.edit_confirm_message({ name: data.editConfig.name }),
			confirm: {
				label: m.common_save(),
				destructive: false,
				action: () => {
					editContainerMutation.mutate(request);
				}
			}
		});
	}
</script>

<div class="flex min-h-0 flex-col bg-background">
	<div class="sticky top-0 z-10 border-b bg-background">
		<div class="mx-auto flex h-16 w-full max-w-full items-center gap-4 px-6">
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				href={`/containers/${data.containerId}`}
				class="gap-2 bg-transparent"
				icon={ArrowLeftIcon}
				customLabel={m.common_back()}
			/>
			<div class="hidden h-4 w-px bg-border sm:block"></div>
			<h1 class="truncate text-base font-semibold">{m.common_edit()} · {data.editConfig.name}</h1>
		</div>
	</div>

	<div class="mx-auto w-full max-w-full px-6 py-6">
		<div class="mb-6 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 dark:text-amber-400">
			{m.edit_recreate_banner()}
		</div>
		<ContainerForm
			mode="edit"
			{form}
			bind:rows
			cancelHref={`/containers/${data.containerId}`}
			submitting={editContainerMutation.isPending}
			onSubmit={handleSubmit}
		/>
	</div>
</div>

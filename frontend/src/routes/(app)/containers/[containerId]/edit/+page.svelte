<script lang="ts">
	import { goto } from '$app/navigation';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import {
		containerFormSchema,
		editContainerFormSchema,
		formValuesFromEditConfig,
		rowsFromEditConfig,
		toEditRequest
	} from '#lib/components/containers/container-form/container-form-state.js';
	import ContainerForm from '#lib/components/containers/container-form/container-form.svelte';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainerEditRequest } from '#lib/types/docker.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { environmentHref } from '#lib/utils/environments.js';
	import { createForm } from '#lib/utils/settings.svelte.js';

	let { data } = $props();

	const queryClient = useQueryClient();
	// The form is intentionally seeded once from the load-time snapshot.
	// svelte-ignore state_referenced_locally
	const form = createForm<typeof containerFormSchema>(
		editContainerFormSchema(data.editConfig.hostname),
		formValuesFromEditConfig(data.editConfig)
	);
	let rows = $state(untrack(() => rowsFromEditConfig(data.editConfig)));

	const editContainerMutation = createMutation(() => ({
		mutationFn: (request: ContainerEditRequest) => containerService.editContainer(data.containerId, request, data.envId),
		onSuccess: async (details) => {
			toast.success(m.edit_success(), activityToastOptions(extractActivityId(details)));
			await queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
			queryClient.removeQueries({ queryKey: queryKeys.containers.detail(data.envId, data.containerId) });
			queryClient.removeQueries({ queryKey: queryKeys.containers.editConfig(data.envId, data.containerId) });
			goto(environmentHref(`/containers/${details.id}`, data.envId));
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
				href={environmentHref(`/containers/${data.containerId}`, data.envId)}
				class="gap-2 bg-transparent"
				icon={ArrowLeftIcon}
				customLabel={m.common_back()}
			/>
			<div class="hidden h-4 w-px bg-border sm:block"></div>
			<h1 class="truncate text-base font-semibold">{m.common_edit()} · {data.editConfig.name}</h1>
			<EnvironmentCell value={data.envId} plain />
		</div>
	</div>

	<div class="mx-auto w-full max-w-full px-6 py-6">
		<div class="mb-6 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 text-sm text-warning">
			{m.edit_recreate_banner()}
		</div>
		<ContainerForm
			mode="edit"
			{form}
			bind:rows
			cancelHref={environmentHref(`/containers/${data.containerId}`, data.envId)}
			environmentId={data.envId}
			hostnamePlaceholder={data.editConfig.id.slice(0, 12)}
			submitting={editContainerMutation.isPending}
			onSubmit={handleSubmit}
		/>
	</div>
</div>

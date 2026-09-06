<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ContainerForm from '#lib/components/containers/container-form/container-form.svelte';
	import {
		containerFormSchema,
		emptyContainerFormRows,
		emptyContainerFormValues,
		toCreateRequest
	} from '#lib/components/containers/container-form/container-form-state.js';
	import { createForm } from '#lib/utils/settings.js';
	import { containerService } from '#lib/services/container-service.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { m } from '#lib/paraglide/messages.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { ArrowLeftIcon } from '#lib/icons/index.js';

	let { data } = $props();

	const queryClient = useQueryClient();
	const form = createForm<typeof containerFormSchema>(containerFormSchema, emptyContainerFormValues());
	let rows = $state(emptyContainerFormRows());

	const createContainerMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.create(data.envId),
		mutationFn: (request: ReturnType<typeof toCreateRequest>) => containerService.createContainer(request, data.envId),
		onSuccess: async (created) => {
			toast.success(m.common_create_success({ resource: m.resource_container() }));
			await queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
			goto(`/containers/${created.id}`);
		},
		onError: (error) => {
			toast.error(m.containers_create_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	function handleSubmit() {
		const values = form.validate();
		if (!values) return;
		createContainerMutation.mutate(toCreateRequest(values, rows));
	}
</script>

<div class="flex min-h-0 flex-col bg-background">
	<div class="sticky top-0 z-10 border-b bg-background">
		<div class="mx-auto flex h-16 w-full max-w-full items-center gap-4 px-6">
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				href="/containers"
				class="gap-2 bg-transparent"
				icon={ArrowLeftIcon}
				customLabel={m.common_back()}
			/>
			<div class="hidden h-4 w-px bg-border sm:block"></div>
			<h1 class="text-base font-semibold">{m.create_container_title()}</h1>
		</div>
	</div>

	<div class="mx-auto w-full max-w-full px-6 py-6">
		<ContainerForm mode="create" {form} bind:rows submitting={createContainerMutation.isPending} onSubmit={handleSubmit} />
	</div>
</div>

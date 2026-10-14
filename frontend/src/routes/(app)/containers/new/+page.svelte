<script lang="ts">
	import { goto } from '$app/navigation';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import {
		containerFormSchema,
		emptyContainerFormRows,
		emptyContainerFormValues,
		toCreateRequest
	} from '#lib/components/containers/container-form/container-form-state.js';
	import ContainerForm from '#lib/components/containers/container-form/container-form.svelte';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { destinationEnvironmentId, environmentHref } from '#lib/utils/environments.js';
	import { createForm } from '#lib/utils/settings.svelte.js';

	const queryClient = useQueryClient();
	const form = createForm<typeof containerFormSchema>(containerFormSchema, emptyContainerFormValues());
	let rows = $state(emptyContainerFormRows());
	let pickedEnvironmentId = $state('');
	const environmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, 'containers:create'));

	const createContainerMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.create(environmentId),
		mutationFn: ({ request, destination }: { request: ReturnType<typeof toCreateRequest>; destination: string }) =>
			containerService.createContainer(request, destination),
		onSuccess: async (created, { destination }) => {
			toast.success(m.common_create_success({ resource: m.resource_container() }));
			await queryClient.invalidateQueries({ queryKey: queryKeys.containers.all });
			goto(environmentHref(`/containers/${created.id}`, destination));
		},
		onError: (error) => {
			toast.error(m.containers_create_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	function handleSubmit() {
		const values = form.validate();
		if (!values) return;
		createContainerMutation.mutate({ request: toCreateRequest(values, rows), destination: environmentId });
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
			<h1 class="flex-1 text-base font-semibold">{m.create_container_title()}</h1>
			<EnvironmentDestinationSelect
				bind:value={() => environmentId, (value) => (pickedEnvironmentId = value)}
				permission="containers:create"
			/>
		</div>
	</div>

	<div class="mx-auto w-full max-w-full space-y-6 px-6 py-6">
		<ContainerForm
			mode="create"
			{form}
			{environmentId}
			bind:rows
			submitting={createContainerMutation.isPending}
			onSubmit={handleSubmit}
		/>
	</div>
</div>

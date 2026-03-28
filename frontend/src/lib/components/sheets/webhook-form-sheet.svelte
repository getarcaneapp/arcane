<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import type { WebhookTargetType, CreateWebhook } from '$lib/types/webhook.type';
	import { containerService } from '$lib/services/container-service';
	import { projectService } from '$lib/services/project-service';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import * as m from '$lib/paraglide/messages.js';

	type WebhookFormProps = {
		open: boolean;
		onSubmit: (data: CreateWebhook) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: WebhookFormProps = $props();

	const targetTypeOptions = $derived([
		{ value: 'container', label: m.webhook_target_type_container(), description: m.webhook_target_type_container_description() },
		{ value: 'project', label: m.webhook_target_type_project(), description: m.webhook_target_type_project_description() },
		{ value: 'updater', label: m.webhook_target_type_updater(), description: m.webhook_target_type_updater_description() },
		{ value: 'gitops', label: m.webhook_target_type_gitops(), description: m.webhook_target_type_gitops_description() }
	]);

	let selectedTargetType = $state<WebhookTargetType>('container');
	let selectedTargetId = $state('');
	let targetOptions = $state<{ label: string; value: string }[]>([]);
	let targetOptionsLoading = $state(false);
	let loadGeneration = 0;

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.webhook_name_label() }))
	});

	let formData = $derived({ name: '' });
	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	$effect(() => {
		if (!open) {
			selectedTargetType = 'container';
			selectedTargetId = '';
			targetOptions = [];
			return;
		}
		loadTargetOptions(selectedTargetType);
	});

	async function loadTargetOptions(type: WebhookTargetType) {
		if (type === 'updater') {
			targetOptions = [];
			selectedTargetId = '';
			return;
		}

		const generation = ++loadGeneration;
		targetOptionsLoading = true;
		try {
			const envId = await environmentStore.getCurrentEnvironmentId();
			let options: { label: string; value: string }[] = [];
			if (type === 'container') {
				const res = await containerService.getContainersForEnvironment(envId, { pagination: { page: 1, limit: 200 } });
				options = res.data.map((c) => ({ value: c.id, label: c.names[0]?.replace(/^\//, '') ?? c.id }));
			} else if (type === 'project') {
				const res = await projectService.getProjectsForEnvironment(envId, { pagination: { page: 1, limit: 200 } });
				options = res.data.map((p) => ({ value: p.id, label: p.name }));
			} else if (type === 'gitops') {
				const res = await gitOpsSyncService.getSyncs(envId, { pagination: { page: 1, limit: 200 } });
				options = res.data.map((s) => ({ value: s.id, label: s.name }));
			}
			if (generation === loadGeneration) {
				targetOptions = options;
				selectedTargetId = options[0]?.value ?? '';
			}
		} catch {
			if (generation === loadGeneration) {
				targetOptions = [];
			}
		} finally {
			if (generation === loadGeneration) {
				targetOptionsLoading = false;
			}
		}
	}

	function handleTargetTypeChange(value: string) {
		selectedTargetType = value as WebhookTargetType;
		selectedTargetId = '';
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		if (selectedTargetType !== 'updater' && !selectedTargetId) return;

		onSubmit({
			name: data.name,
			targetType: selectedTargetType,
			targetId: selectedTargetId
		});
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
	}
</script>

<ResponsiveDialog.Root
	{open}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.webhook_create_title()}
	description={m.webhook_create_description()}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.webhook_name_label()}
				type="text"
				placeholder={m.webhook_name_placeholder()}
				description={m.webhook_name_description()}
				bind:input={$inputs.name}
			/>

			<SelectWithLabel
				id="webhook-target-type"
				label={m.webhook_target_type_label()}
				description={m.webhook_target_type_description()}
				value={selectedTargetType}
				options={targetTypeOptions}
				onValueChange={handleTargetTypeChange}
			/>

			{#if selectedTargetType !== 'updater'}
				<SelectWithLabel
					id="webhook-target-id"
					label={selectedTargetType === 'container'
						? m.webhook_target_resource_label_container()
						: selectedTargetType === 'project'
							? m.webhook_target_resource_label_project()
							: m.webhook_target_resource_label_gitops()}
					description={m.webhook_target_resource_description()}
					bind:value={selectedTargetId}
					options={targetOptions}
					disabled={targetOptionsLoading || targetOptions.length === 0}
					placeholder={targetOptionsLoading ? m.webhook_target_resource_loading() : m.webhook_target_resource_placeholder()}
				/>
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="outline"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			<ArcaneButton
				action="create"
				type="submit"
				class="flex-1"
				disabled={isLoading || (selectedTargetType !== 'updater' && !selectedTargetId)}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={m.webhook_create_button()}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>

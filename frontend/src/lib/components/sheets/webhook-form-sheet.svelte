<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import type { WebhookActionType, WebhookTargetType, CreateWebhook } from '$lib/types/webhook.type';
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

	function getDefaultActionType(targetType: WebhookTargetType): WebhookActionType {
		switch (targetType) {
			case 'container':
			case 'project':
				return 'update';
			case 'updater':
				return 'run';
			case 'gitops':
				return 'sync';
			default:
				return 'update';
		}
	}

	function getActionTypeOptions(
		targetType: WebhookTargetType
	): { value: WebhookActionType; label: string; description: string }[] {
		switch (targetType) {
			case 'container':
				return [
					{ value: 'update', label: m.webhook_action_type_update(), description: m.webhook_action_type_update_description() },
					{ value: 'start', label: m.webhook_action_type_start(), description: m.webhook_action_type_start_description() },
					{ value: 'stop', label: m.webhook_action_type_stop(), description: m.webhook_action_type_stop_description() },
					{ value: 'restart', label: m.webhook_action_type_restart(), description: m.webhook_action_type_restart_description() },
					{
						value: 'redeploy',
						label: m.webhook_action_type_redeploy(),
						description: m.webhook_action_type_redeploy_description()
					}
				];
			case 'project':
				return [
					{ value: 'update', label: m.webhook_action_type_update(), description: m.webhook_action_type_update_description() },
					{ value: 'up', label: m.webhook_action_type_up(), description: m.webhook_action_type_up_description() },
					{ value: 'down', label: m.webhook_action_type_down(), description: m.webhook_action_type_down_description() },
					{ value: 'restart', label: m.webhook_action_type_restart(), description: m.webhook_action_type_restart_description() },
					{
						value: 'redeploy',
						label: m.webhook_action_type_redeploy(),
						description: m.webhook_action_type_redeploy_description()
					}
				];
			case 'updater':
				return [{ value: 'run', label: m.webhook_action_type_run(), description: m.webhook_action_type_run_description() }];
			case 'gitops':
				return [{ value: 'sync', label: m.webhook_action_type_sync(), description: m.webhook_action_type_sync_description() }];
			default:
				return [];
		}
	}

	let selectedTargetType = $state<WebhookTargetType>('container');
	let selectedActionType = $state<WebhookActionType>(getDefaultActionType('container'));
	let selectedTargetId = $state('');
	let targetOptions = $state<{ label: string; value: string }[]>([]);
	let targetOptionsLoading = $state(false);
	let loadGeneration = 0;
	let actionTypeOptions = $derived(getActionTypeOptions(selectedTargetType));

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.webhook_name_label() }))
	});

	let formData = $derived({ name: '' });
	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

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
		selectedActionType = getDefaultActionType(selectedTargetType);
		selectedTargetId = '';
		void loadTargetOptions(selectedTargetType);
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		if (selectedTargetType !== 'updater' && !selectedTargetId) return;

		onSubmit({
			name: data.name,
			targetType: selectedTargetType,
			actionType: selectedActionType,
			targetId: selectedTargetId
		});
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (newOpenState) {
			void loadTargetOptions(selectedTargetType);
			return;
		}
		if (!newOpenState) {
			selectedTargetType = 'container';
			selectedActionType = getDefaultActionType('container');
			selectedTargetId = '';
			targetOptions = [];
		}
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

			<SelectWithLabel
				id="webhook-action-type"
				label={m.webhook_action_type_label()}
				description={m.webhook_action_type_description()}
				value={selectedActionType}
				options={actionTypeOptions}
				onValueChange={(value) => (selectedActionType = value as WebhookActionType)}
				disabled={actionTypeOptions.length === 1}
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

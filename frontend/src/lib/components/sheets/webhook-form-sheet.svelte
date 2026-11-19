<script lang="ts">
	import { z } from 'zod/v4';

	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { containerService } from '#lib/services/container-service.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { secretSourceService } from '#lib/services/secret-source-service.js';
	import type { WebhookCreate } from '#lib/types/ui/webhook.js';
	import type { WebhookActionType, WebhookTargetType } from '#lib/types/webhook.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	type WebhookFormProps = {
		open: boolean;
		onSubmit: (data: WebhookCreate, environmentId: string) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: WebhookFormProps = $props();

	const targetTypeOptions = $derived([
		{ value: 'container', label: m.container(), description: m.webhook_target_type_container_description() },
		{ value: 'project', label: m.project(), description: m.webhook_target_type_project_description() },
		{ value: 'updater', label: m.updater(), description: m.webhook_target_type_updater_description() },
		{ value: 'gitops', label: m.gitops(), description: m.webhook_target_type_gitops_description() },
		{ value: 'secretsource', label: m.external_secret(), description: m.refresh_external_secret_target_description() }
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
			case 'secretsource':
				return 'refresh';
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
					{ value: 'update', label: m.common_update(), description: m.webhook_action_type_update_description() },
					{ value: 'start', label: m.common_start(), description: m.webhook_action_type_start_description() },
					{ value: 'stop', label: m.common_stop(), description: m.webhook_action_type_stop_description() },
					{ value: 'restart', label: m.common_restart(), description: m.webhook_action_type_restart_description() },
					{
						value: 'redeploy',
						label: m.common_redeploy(),
						description: m.webhook_action_type_redeploy_description()
					}
				];
			case 'project':
				return [
					{ value: 'update', label: m.common_update(), description: m.webhook_action_type_update_description() },
					{ value: 'up', label: m.common_up(), description: m.webhook_action_type_up_description() },
					{ value: 'down', label: m.common_down(), description: m.webhook_action_type_down_description() },
					{ value: 'restart', label: m.common_restart(), description: m.webhook_action_type_restart_description() },
					{
						value: 'redeploy',
						label: m.common_redeploy(),
						description: m.webhook_action_type_redeploy_description()
					}
				];
			case 'updater':
				return [{ value: 'run', label: m.webhook_action_type_run(), description: m.webhook_action_type_run_description() }];
			case 'gitops':
				return [{ value: 'sync', label: m.resource_sync_cap(), description: m.webhook_action_type_sync_description() }];
			case 'secretsource':
				return [{ value: 'refresh', label: m.common_refresh(), description: m.refresh_external_secret_action_description() }];
			default:
				return [];
		}
	}

	let pickedEnvironmentId = $state('');
	const environmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, 'webhooks:create'));
	let selectedTargetType = $state<WebhookTargetType>('container');
	let selectedActionType = $state<WebhookActionType>(getDefaultActionType('container'));
	let selectedTargetId = $state('');
	let targetOptions = $state<{ label: string; value: string }[]>([]);
	let targetOptionsLoading = $state(false);
	let loadGeneration = 0;
	let actionTypeOptions = $derived(getActionTypeOptions(selectedTargetType));
	const targetLabel = $derived.by(() => {
		if (selectedTargetType === 'container') return m.container();
		if (selectedTargetType === 'project') return m.project();
		if (selectedTargetType === 'secretsource') return m.external_secret();
		return m.webhook_target_resource_label_gitops();
	});

	const formSchema = z.object({
		name: z.string().min(1, m.common_field_required({ field: m.common_name() }))
	});

	const form = createForm<typeof formSchema>(formSchema, { name: '' });

	async function fetchTargetOptions(type: WebhookTargetType, envId: string) {
		const request = { pagination: { page: 1, limit: 200 } };
		if (type === 'container') {
			const res = await containerService.getContainersForEnvironment(envId, request);
			return res.data.map((c) => ({ value: c.id, label: c.names[0] ?? c.id }));
		}
		if (type === 'project') {
			const res = await projectService.getProjectsForEnvironment(envId, request);
			return res.data.map((p) => ({ value: p.id, label: p.name }));
		}
		if (type === 'gitops') {
			const res = await gitOpsSyncService.getSyncs(envId, request);
			return res.data.map((s) => ({ value: s.id, label: s.name }));
		}
		if (type === 'secretsource' && envId === '0') {
			const sources = await secretSourceService.list();
			return sources.map((s) => ({ value: s.id, label: s.name }));
		}
		return [];
	}

	async function loadTargetOptions(type: WebhookTargetType, envId: string) {
		if (type === 'updater') {
			targetOptions = [];
			selectedTargetId = '';
			return;
		}

		const generation = ++loadGeneration;
		targetOptionsLoading = true;
		try {
			const options = await tryCatch(fetchTargetOptions(type, envId));
			if (generation !== loadGeneration) return;
			if (options.error !== null) {
				targetOptions = [];
				return;
			}
			targetOptions = options.data;
			selectedTargetId = options.data[0]?.value ?? '';
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
		void loadTargetOptions(selectedTargetType, environmentId);
	}

	function handleEnvironmentChange(value: string) {
		pickedEnvironmentId = value;
		selectedTargetId = '';
		void loadTargetOptions(selectedTargetType, environmentId);
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		if (selectedTargetType !== 'updater' && !selectedTargetId) return;

		onSubmit(
			{
				name: data.name,
				targetType: selectedTargetType,
				actionType: selectedActionType,
				targetId: selectedTargetId
			},
			environmentId
		);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (newOpenState) {
			void loadTargetOptions(selectedTargetType, environmentId);
			return;
		}
		if (!newOpenState) {
			pickedEnvironmentId = '';
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
	title={m.create_webhook()}
	description={m.webhook_create_description()}
	contentClass="sm:max-w-125"
>
	<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
		<EnvironmentDestinationSelect
			bind:value={() => environmentId, handleEnvironmentChange}
			permission="webhooks:create"
			variant="field"
		/>
		<FormInput
			label={m.common_name()}
			type="text"
			placeholder={m.webhook_name_placeholder()}
			description={m.webhook_name_description()}
			bind:input={form.inputs.name}
		/>

		<SelectWithLabel
			id="webhook-target-type"
			label={m.target_type()}
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
				label={targetLabel}
				description={m.webhook_target_resource_description()}
				bind:value={selectedTargetId}
				options={targetOptions}
				disabled={targetOptionsLoading || targetOptions.length === 0}
				placeholder={targetOptionsLoading ? m.webhook_target_resource_loading() : m.webhook_target_resource_placeholder()}
			/>
		{/if}
	</form>

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={isLoading}
			submitDisabled={isLoading || (selectedTargetType !== 'updater' && !selectedTargetId)}
			submitLoading={isLoading}
			onSubmit={handleSubmit}
			submitLabel={m.create_webhook()}
		/>
	{/snippet}
</ResponsiveDialog.Root>

<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import KeyValueEditor from '#lib/components/form/key-value-editor.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { InfoIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { SwarmKvSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { keyValueRowsToRecord, recordToKeyValueRows } from '#lib/utils/form-parsers.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { preventDefault } from '#lib/utils/settings.svelte.js';
	import { decodeBase64ToText, encodeTextToBase64, type SwarmKvResource } from '#lib/utils/swarm-kv.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		resource,
		environmentId = $bindable(),
		environments,
		existing,
		onSaved
	}: {
		open: boolean;
		resource: SwarmKvResource;
		environmentId: string;
		environments?: EnvironmentRef[];
		existing: SwarmKvSummary | null;
		onSaved: () => Promise<void>;
	} = $props();

	const isSecret = $derived(resource.kind === 'secrets');
	const canManage = $derived(hasPermission(resource.permission, environmentId));

	const initial = untrack(() => existing?.spec);
	let name = $state(initial?.Name ?? '');
	let data = $state(decodeBase64ToText(initial?.Data ?? ''));
	let labels = $state(recordToKeyValueRows(initial?.Labels));
	let submitting = $state(false);

	const description = $derived(
		existing
			? [
					existing.spec.Name,
					existing.spec.Driver?.Name,
					`${m.common_created()} ${formatRelativeTime(existing.createdAt) || m.common_unknown()}`,
					`${m.common_updated()} ${formatRelativeTime(existing.updatedAt) || m.common_unknown()}`
				]
					.filter(Boolean)
					.join(' · ')
			: resource.subtitle()
	);

	async function handleSubmit() {
		if (existing) {
			const resourceName = `${resource.resource()} "${existing.spec.Name}"`;
			submitting = true;
			await handleApiResultWithCallbacks({
				result: await tryCatch(
					swarmService.updateKvItem(
						resource.kind,
						existing.id,
						{
							version: existing.version.Index ?? 0,
							labels: keyValueRowsToRecord(labels)
						},
						environmentId
					)
				),
				message: m.common_update_failed({ resource: resourceName }),
				setLoadingState: (value) => (submitting = value),
				onSuccess: async () => {
					toast.success(m.common_update_success({ resource: resourceName }));
					await onSaved();
				}
			});
			return;
		}

		const trimmedName = name.trim();
		if (!trimmedName) {
			toast.error(resource.nameRequired());
			return;
		}
		submitting = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				swarmService.createKvItem(
					resource.kind,
					{
						spec: {
							Name: trimmedName,
							Labels: keyValueRowsToRecord(labels),
							Data: encodeTextToBase64(data)
						}
					},
					environmentId
				)
			),
			message: resource.createFailed(),
			setLoadingState: (value) => (submitting = value),
			onSuccess: async () => {
				toast.success(resource.createSuccess(trimmedName));
				await onSaved();
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={existing ? resource.editTitle() : resource.createLabel()}
	{description}
	contentClass="sm:max-w-150"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			{#if !existing}
				<EnvironmentDestinationSelect
					bind:value={environmentId}
					permission={resource.permission}
					{environments}
					variant="field"
				/>
			{/if}
			<TextInputWithLabel
				id="swarm-kv-name"
				label={m.common_name()}
				placeholder={resource.namePlaceholder()}
				bind:value={name}
				helpText={existing ? resource.nameLocked() : undefined}
				disabled={!!existing || submitting || !canManage}
				required={!existing}
			/>

			<div class="space-y-2.5">
				<div>
					<Label>{m.common_labels()}</Label>
					<p class="mt-0.5 text-xs text-muted-foreground">{m.common_labels_description({ resource: resource.resource() })}</p>
				</div>
				<KeyValueEditor
					bind:rows={labels}
					keyPlaceholder={m.swarm_secrets_label_key_placeholder()}
					valuePlaceholder={m.swarm_secrets_label_value_placeholder()}
					addLabel={m.add_label()}
					disabled={submitting || !canManage}
				/>
			</div>

			{#if existing && isSecret}
				<Alert.Root variant="primary-subtle">
					<InfoIcon class="size-4" />
					<Alert.Description>{resource.immutableNotice()}</Alert.Description>
				</Alert.Root>
			{:else}
				<div class="space-y-2.5">
					<Label for="swarm-kv-data">{isSecret ? m.value() : m.swarm_configs_content_label()}</Label>
					<Textarea
						id="swarm-kv-data"
						rows={isSecret ? 6 : 14}
						mono
						size="sm"
						bind:value={data}
						placeholder={isSecret ? m.swarm_secrets_data_placeholder() : m.swarm_configs_data_placeholder()}
						readonly={!!existing || !canManage}
						disabled={submitting}
					/>
					{#if existing}
						<p class="text-xs text-muted-foreground">{resource.immutableNotice()}</p>
					{:else if isSecret}
						<p class="text-xs text-muted-foreground">{m.swarm_secrets_value_help()}</p>
					{/if}
				</div>
			{/if}
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={submitting}
			showSubmit={canManage}
			submitAction={existing ? 'save' : 'create'}
			onSubmit={handleSubmit}
			submitDisabled={submitting}
			submitLoading={submitting}
			submitLabel={existing ? m.common_save_changes() : resource.createLabel()}
		/>
	{/snippet}
</ResponsiveDialog.Root>

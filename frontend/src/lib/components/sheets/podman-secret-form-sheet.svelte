<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import KeyValueEditor from '#lib/components/form/key-value-editor.svelte';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanSecret } from '#lib/types/podman.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { destinationEnvironmentId, type EnvironmentRow } from '#lib/utils/environments.js';
	import { keyValueRowsToRecord, recordToKeyValueRows } from '#lib/utils/form-parsers.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';
	import { preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let {
		open = $bindable(false),
		environments,
		existing,
		onSaved
	}: {
		open: boolean;
		environments: EnvironmentRef[];
		existing: EnvironmentRow<PodmanSecret> | null;
		onSaved: () => Promise<void>;
	} = $props();

	const initial = untrack(() => existing?.spec);
	let pickedEnvironmentId = $state('');
	let name = $state(initial?.name ?? '');
	let driver = $state(initial?.driver.name ?? '');
	let driverOptions = $state(recordToKeyValueRows(initial?.driver.options));
	let labels = $state(recordToKeyValueRows(initial?.labels));
	let value = $state('');
	let submitting = $state(false);

	const environmentId = $derived(
		existing?.environmentId ?? destinationEnvironmentId(pickedEnvironmentId, PERMISSIONS.PODMAN_SECRETS, environments)
	);
	const canManage = $derived(hasPermission(PERMISSIONS.PODMAN_SECRETS, environmentId));
	const valueLabel = $derived(existing ? m.podman_secrets_new_value_label() : m.value());

	const description = $derived(
		existing
			? [
					existing.spec.name,
					existing.environmentName,
					existing.spec.driver.name,
					`${m.common_created()} ${formatRelativeTime(existing.createdAt) || m.common_unknown()}`,
					`${m.common_updated()} ${formatRelativeTime(existing.updatedAt) || m.common_unknown()}`
				]
					.filter(Boolean)
					.join(' · ')
			: m.podman_secrets_subtitle()
	);

	async function handleSubmit() {
		const trimmedName = name.trim();
		if (!trimmedName) {
			toast.error(m.swarm_secrets_name_required());
			return;
		}
		if (!value) {
			toast.error(m.common_field_required({ field: valueLabel }));
			return;
		}
		const request = {
			data: value,
			driver: driver.trim() || undefined,
			driverOptions: keyValueRowsToRecord(driverOptions),
			labels: keyValueRowsToRecord(labels)
		};
		const resourceName = `${m.swarm_secret()} "${trimmedName}"`;
		submitting = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(
				existing
					? podmanService.updateSecret(trimmedName, request, environmentId)
					: podmanService.createSecret({ ...request, name: trimmedName }, environmentId)
			),
			message: existing ? m.common_update_failed({ resource: resourceName }) : m.swarm_secrets_create_failed(),
			setLoadingState: (loading) => (submitting = loading),
			onSuccess: async () => {
				toast.success(
					existing ? m.common_update_success({ resource: resourceName }) : m.swarm_secrets_create_success({ name: trimmedName })
				);
				await onSaved();
			}
		});
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={existing ? m.swarm_secrets_edit_title() : m.create_secret()}
	{description}
	contentClass="sm:max-w-150"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			{#if !existing}
				<EnvironmentDestinationSelect
					bind:value={() => environmentId, (next) => (pickedEnvironmentId = next)}
					permission={PERMISSIONS.PODMAN_SECRETS}
					{environments}
					variant="field"
				/>
			{/if}

			<TextInputWithLabel
				id="podman-secret-name"
				label={m.common_name()}
				placeholder={m.swarm_secrets_name_placeholder()}
				bind:value={name}
				helpText={existing ? m.swarm_secrets_name_locked() : undefined}
				disabled={!!existing || submitting || !canManage}
				required={!existing}
			/>

			<TextInputWithLabel
				id="podman-secret-driver"
				label={m.common_driver()}
				placeholder={m.podman_secrets_driver_placeholder()}
				bind:value={driver}
				disabled={submitting || !canManage}
			/>

			<div class="space-y-2.5">
				<Label>{m.common_driver_options()}</Label>
				<KeyValueEditor
					bind:rows={driverOptions}
					keyPlaceholder={m.podman_secrets_driver_option_key_placeholder()}
					valuePlaceholder={m.podman_secrets_driver_option_value_placeholder()}
					disabled={submitting || !canManage}
				/>
			</div>

			<div class="space-y-2.5">
				<div>
					<Label>{m.common_labels()}</Label>
					<p class="mt-0.5 text-xs text-muted-foreground">{m.common_labels_description({ resource: m.swarm_secret() })}</p>
				</div>
				<KeyValueEditor
					bind:rows={labels}
					keyPlaceholder={m.swarm_secrets_label_key_placeholder()}
					valuePlaceholder={m.swarm_secrets_label_value_placeholder()}
					addLabel={m.add_label()}
					disabled={submitting || !canManage}
				/>
			</div>

			<div class="space-y-2.5">
				<Label for="podman-secret-value">
					{valueLabel}{#if !existing}<span class="ml-0.5 text-destructive">*</span>{/if}
				</Label>
				<Textarea
					id="podman-secret-value"
					rows={6}
					mono
					size="sm"
					autocomplete="off"
					spellcheck={false}
					bind:value
					placeholder={existing ? m.podman_secrets_new_value_placeholder() : m.swarm_secrets_data_placeholder()}
					disabled={submitting || !canManage}
				/>
				<p class="text-xs text-muted-foreground">
					{existing ? m.podman_secrets_new_value_help() : m.swarm_secrets_value_help()}
				</p>
			</div>
		</form>
	{/snippet}

	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={submitting}
			showSubmit={canManage}
			submitAction={existing ? 'save' : 'create'}
			submitDisabled={submitting || !value}
			submitLoading={submitting}
			onSubmit={handleSubmit}
			submitLabel={existing ? m.common_save_changes() : m.create_secret()}
		/>
	{/snippet}
</ResponsiveDialog.Root>

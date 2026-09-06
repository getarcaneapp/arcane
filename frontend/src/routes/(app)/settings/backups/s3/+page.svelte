<script lang="ts">
	import { toast } from 'svelte-sonner';
	import settingsStore from '#lib/stores/config-store.js';
	import { SettingsPageLayout, type SettingsActionButton } from '#lib/layouts/index.js';
	import { RemoteEnvironmentIcon } from '#lib/icons/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { s3DestinationService } from '#lib/services/s3-destination-service.js';
	import type { CreateS3Destination, S3Destination } from '#lib/types/s3-destination.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import * as m from '#lib/paraglide/messages.js';
	import S3DestinationDialog from './s3-destination-dialog.svelte';
	import S3DestinationTable from './s3-destination-table.svelte';

	let { data } = $props();
	let destinations = $derived(data.destinations);
	let requestOptions = $derived<SearchPaginationSortRequest>(data.requestOptions);
	let selected = $state<S3Destination | null>(null);
	let dialogOpen = $state(false);
	let saving = $state(false);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);

	function openCreate() {
		selected = null;
		dialogOpen = true;
	}

	function openEdit(destination: S3Destination) {
		selected = destination;
		dialogOpen = true;
	}

	async function saveDestination(input: CreateS3Destination) {
		saving = true;
		try {
			if (selected) {
				await s3DestinationService.update(selected.id, input);
				toast.success(m.s3_destination_updated({ name: input.name }));
			} else {
				await s3DestinationService.create(input);
				toast.success(m.s3_destination_created({ name: input.name }));
			}
			destinations = await s3DestinationService.list(requestOptions);
			dialogOpen = false;
			selected = null;
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destination_save_failed());
		} finally {
			saving = false;
		}
	}

	function deleteDestination(destination: S3Destination) {
		openConfirmDialog({
			title: m.delete_name({ name: destination.name }),
			message: m.s3_destination_delete_message(),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					try {
						await s3DestinationService.delete(destination.id);
						destinations = await s3DestinationService.list(requestOptions);
						toast.success(m.s3_destination_deleted({ name: destination.name }));
					} catch (error) {
						toast.error(error instanceof Error ? error.message : m.s3_destination_delete_failed());
					}
				}
			}
		});
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => [
		{
			id: 'create',
			action: 'create',
			label: m.s3_destination_add(),
			onclick: openCreate,
			disabled: isReadOnly
		}
	]);
</script>

<SettingsPageLayout
	title={m.s3_destinations_title()}
	description={m.s3_destinations_description()}
	icon={RemoteEnvironmentIcon}
	pageType="management"
	showReadOnlyTag={isReadOnly}
	{actionButtons}
>
	{#snippet mainContent()}
		<S3DestinationTable
			bind:destinations
			bind:requestOptions
			onDestinationsChanged={(options) => s3DestinationService.list(options)}
			onEdit={openEdit}
			onDelete={deleteDestination}
		/>
	{/snippet}
	{#snippet additionalContent()}
		<S3DestinationDialog bind:open={dialogOpen} destination={selected} {saving} onSubmit={saveDestination} />
	{/snippet}
</SettingsPageLayout>

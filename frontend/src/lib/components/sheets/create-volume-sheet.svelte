<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import * as Accordion from '$lib/components/ui/accordion/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { VolumeCreateRequest } from '$lib/types/volume.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import SelectWithLabel from '../form/select-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, VolumesIcon } from '$lib/icons';

	type CreateVolumeFormProps = {
		open: boolean;
		onSubmit: (data: VolumeCreateRequest) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: CreateVolumeFormProps = $props();

	const drivers = [
		{ value: 'local', label: m.volume_driver_local() },
		{ value: 'nfs', label: m.volume_driver_nfs() },
		{ value: 'awsElasticBlockStore', label: m.volume_driver_aws_ebs() },
		{ value: 'azure_disk', label: m.volume_driver_azure_disk() },
		{ value: 'gcePersistentDisk', label: m.volume_driver_gce_pd() }
	];

	const formSchema = z.object({
		volumeName: z.string().min(1, m.volume_name_required()),
		volumeDriver: z.string().min(1, m.common_driver_required()),
		volumeOptText: z.string().optional().default(''),
		volumeLabels: z.string().optional().default('')
	});

	let formData = $derived({
		volumeName: '',
		volumeDriver: 'local',
		volumeOptText: '',
		volumeLabels: ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function parseKeyValuePairs(text: string): Record<string, string> {
		if (!text?.trim()) return {};

		const result: Record<string, string> = {};
		const lines = text.split('\n');

		for (const line of lines) {
			const trimmed = line.trim();
			if (!trimmed || !trimmed.includes('=')) continue;

			const [key, ...valueParts] = trimmed.split('=');
			const value = valueParts.join('=');

			if (key.trim()) {
				result[key.trim()] = value.trim();
			}
		}

		return result;
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const driverOpts = parseKeyValuePairs(data.volumeOptText || '');
		const labels = parseKeyValuePairs(data.volumeLabels || '');

		const volumeOptions: VolumeCreateRequest = {
			name: data.volumeName.trim(),
			driver: data.volumeDriver,
			driverOpts: Object.keys(driverOpts).length ? driverOpts : undefined,
			labels: Object.keys(labels).length ? labels : undefined
		};

		onSubmit(volumeOptions);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			$inputs.volumeName.value = '';
			$inputs.volumeDriver.value = 'local';
			$inputs.volumeOptText.value = '';
			$inputs.volumeLabels.value = '';
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.create_volume_title()}
	description={m.create_volume_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput
				label={m.volume_name_label()}
				id="volume-name"
				type="text"
				placeholder={m.volume_name_placeholder()}
				description={m.volume_name_description()}
				disabled={isLoading}
				bind:input={$inputs.volumeName}
			/>

			<SelectWithLabel
				id="driver-select"
				bind:value={$inputs.volumeDriver.value}
				label={m.volume_driver_label()}
				description={m.volume_driver_description()}
				options={drivers}
				placeholder={m.volume_driver_placeholder()}
			/>

			<Accordion.Root type="single" class="w-full">
				<Accordion.Item value="advanced">
					<Accordion.Trigger class="text-sm font-medium">{m.volume_advanced_settings()}</Accordion.Trigger>
					<Accordion.Content class="pt-4">
						<div class="space-y-4">
							<FormInput
								label={m.common_driver_options()}
								type="textarea"
								placeholder={m.volume_driver_options_placeholder()}
								description={m.volume_driver_options_description()}
								disabled={isLoading}
								rows={3}
								bind:input={$inputs.volumeOptText}
							/>

							<FormInput
								label={m.common_labels()}
								type="textarea"
								placeholder={m.volume_labels_placeholder()}
								description={m.volumes_labels_description()}
								disabled={isLoading}
								rows={3}
								bind:input={$inputs.volumeLabels}
							/>
						</div>
					</Accordion.Content>
				</Accordion.Item>
			</Accordion.Root>
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
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={m.common_create_button({ resource: m.resource_volume_cap() })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>

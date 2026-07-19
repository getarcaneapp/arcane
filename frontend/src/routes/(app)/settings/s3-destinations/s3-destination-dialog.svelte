<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog';
	import SheetFooterActions from '$lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import LabeledSwitch from '$lib/components/form/labeled-switch.svelte';
	import type { CreateS3Destination, S3Destination } from '$lib/types/s3-destination';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { z } from 'zod/v4';
	import * as m from '$lib/paraglide/messages.js';

	let {
		open = $bindable(),
		destination,
		saving,
		onSubmit
	}: {
		open: boolean;
		destination: S3Destination | null;
		saving: boolean;
		onSubmit: (input: CreateS3Destination) => Promise<void>;
	} = $props();

	const formSchema = z
		.object({
			name: z.string().trim().min(1, m.s3_destination_name_required()),
			endpoint: z.string(),
			bucket: z.string().trim().min(1, m.backups_s3_bucket_required()),
			region: z.string().trim().min(1, m.backups_s3_region_required()),
			accessKeyId: z.string().trim().min(1, m.backups_s3_access_key_required()),
			secretAccessKey: z.string(),
			prefix: z.string(),
			useSsl: z.boolean(),
			forcePathStyle: z.boolean()
		})
		.superRefine((data, ctx) => {
			if (!destination?.secretConfigured && !data.secretAccessKey.trim()) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					message: m.backups_s3_secret_key_required(),
					path: ['secretAccessKey']
				});
			}
		});

	let formData = $derived<CreateS3Destination>({
		name: open && destination ? destination.name : '',
		endpoint: open && destination ? (destination.endpoint ?? '') : '',
		bucket: open && destination ? destination.bucket : '',
		region: open && destination ? destination.region : 'us-east-1',
		accessKeyId: open && destination ? destination.accessKeyId : '',
		secretAccessKey: '',
		prefix: open && destination ? (destination.prefix ?? '') : '',
		useSsl: open && destination ? destination.useSsl : true,
		forcePathStyle: open && destination ? destination.forcePathStyle : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		onSubmit(data);
	}
</script>

<ResponsiveDialog
	bind:open
	variant="sheet"
	title={destination ? m.s3_destination_edit_title() : m.s3_destination_add_title()}
	description={m.s3_destination_dialog_description()}
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<FormInput bind:input={$inputs.name} label={m.common_name()} />
			<FormInput
				bind:input={$inputs.endpoint}
				label={m.backups_s3_endpoint_label()}
				description={m.backups_s3_endpoint_description()}
			/>
			<FormInput bind:input={$inputs.bucket} label={m.backups_s3_bucket_label()} />
			<FormInput bind:input={$inputs.region} label={m.backups_s3_region_label()} />
			<FormInput bind:input={$inputs.accessKeyId} label={m.backups_s3_access_key_label()} autocomplete="off" />
			<FormInput
				bind:input={$inputs.secretAccessKey}
				label={m.backups_s3_secret_key_label()}
				description={destination?.secretConfigured ? m.s3_destination_secret_keep_description() : undefined}
				type="password"
				autocomplete="new-password"
			/>
			<FormInput
				bind:input={$inputs.prefix}
				label={m.backups_s3_prefix_label()}
				description={m.backups_s3_prefix_description()}
			/>
			<div class="mt-1 grid gap-4 border-t pt-5 sm:grid-cols-2">
				<LabeledSwitch
					id="s3-destination-ssl"
					bind:checked={$inputs.useSsl.value}
					label={m.backups_s3_ssl_label()}
					description={m.backups_s3_ssl_description()}
				/>
				<LabeledSwitch
					id="s3-destination-path-style"
					bind:checked={$inputs.forcePathStyle.value}
					label={m.backups_s3_path_style_label()}
					description={m.backups_s3_path_style_description()}
				/>
			</div>
		</form>
	{/snippet}
	{#snippet footer()}
		<SheetFooterActions
			bind:open
			cancelDisabled={saving}
			submitAction={destination ? 'save' : 'create'}
			submitDisabled={saving}
			submitLoading={saving}
			onSubmit={handleSubmit}
			submitLabel={destination ? m.common_save_changes() : m.s3_destination_add_title()}
		/>
	{/snippet}
</ResponsiveDialog>

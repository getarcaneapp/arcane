<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import FormInput from '#lib/components/form/form-input.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import type { CreateS3Destination, S3Destination } from '#lib/types/s3-destination.js';
	import { createForm, preventDefault } from '#lib/utils/settings.js';
	import { s3DestinationService } from '#lib/services/s3-destination-service.js';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import * as m from '#lib/paraglide/messages.js';

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
			name: z.string().trim().min(1, m.common_name_required()),
			endpoint: z.string(),
			bucket: z.string().trim().min(1, m.backups_s3_bucket_required()),
			region: z.string(),
			accessKeyId: z.string().trim().min(1, m.backups_s3_access_key_required()),
			secretAccessKey: z.string(),
			prefix: z.string(),
			useSsl: z.boolean(),
			forcePathStyle: z.boolean()
		})
		.superRefine((data, ctx) => {
			if (!data.endpoint.trim() && !data.region.trim()) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					message: m.backups_s3_region_required(),
					path: ['region']
				});
			}
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
	let testing = $state(false);
	let testedConfiguration = $state<string | null>(null);
	const currentConfiguration = $derived(
		JSON.stringify({
			endpoint: $inputs.endpoint.value.trim(),
			bucket: $inputs.bucket.value.trim(),
			region: $inputs.region.value.trim(),
			accessKeyId: $inputs.accessKeyId.value.trim(),
			secretAccessKey: $inputs.secretAccessKey.value.trim(),
			prefix: $inputs.prefix.value.trim().replace(/^\/+|\/+$/g, ''),
			useSsl: $inputs.useSsl.value,
			forcePathStyle: $inputs.forcePathStyle.value
		})
	);
	const connectionVerified = $derived(testedConfiguration === currentConfiguration);

	$effect(() => {
		open;
		destination?.id;
		testedConfiguration = null;
	});

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;
		if (!connectionVerified) {
			toast.error(m.s3_destination_test_required());
			return;
		}
		onSubmit(data);
	}

	async function handleTest() {
		const data = form.validate();
		if (!data) return;
		const testedCandidate = currentConfiguration;
		testedConfiguration = null;
		testing = true;
		try {
			if (destination) {
				await s3DestinationService.test(destination.id, data);
			} else {
				await s3DestinationService.testConfiguration(data);
			}
			testedConfiguration = testedCandidate;
			toast.success(m.s3_destination_test_success({ name: data.name }));
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.s3_destination_test_failed({ name: data.name }));
		} finally {
			testing = false;
		}
	}
</script>

{#snippet destinationFields()}
	<FormInput bind:input={$inputs.name} label={m.common_name()} />
	<FormInput
		bind:input={$inputs.endpoint}
		label={m.backups_s3_endpoint_label()}
		description={m.backups_s3_endpoint_description()}
	/>
	<FormInput bind:input={$inputs.bucket} label={m.backups_s3_bucket_label()} />
	<FormInput bind:input={$inputs.region} label={m.backups_s3_region_label()} description={m.backups_s3_region_description()} />
{/snippet}

{#snippet credentialFields()}
	<FormInput bind:input={$inputs.accessKeyId} label={m.backups_s3_access_key_label()} autocomplete="off" />
	<FormInput
		bind:input={$inputs.secretAccessKey}
		label={m.backups_s3_secret_key_label()}
		description={destination?.secretConfigured ? m.s3_destination_secret_keep_description() : undefined}
		type="password"
		autocomplete="new-password"
	/>
{/snippet}

{#snippet connectionOptions()}
	<FormInput bind:input={$inputs.prefix} label={m.backups_s3_prefix_label()} description={m.backups_s3_prefix_description()} />
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
{/snippet}

<ResponsiveDialog
	bind:open
	variant="sheet"
	title={destination ? m.s3_destination_edit_title() : m.s3_destination_add_title()}
	description={m.s3_destination_dialog_description()}
	contentClass="sm:max-w-[640px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			{@render destinationFields()}
			{@render credentialFields()}
			{@render connectionOptions()}
		</form>
	{/snippet}
	{#snippet footer()}
		<div class="flex w-full flex-col gap-2">
			<p class={connectionVerified ? 'text-xs text-green-600' : 'text-xs text-muted-foreground'}>
				{connectionVerified ? m.s3_destination_test_verified() : m.s3_destination_test_required()}
			</p>
			<SheetFooterActions
				bind:open
				cancelDisabled={saving || testing}
				submitAction={connectionVerified ? (destination ? 'save' : 'create') : 'test'}
				submitDisabled={saving || testing}
				submitLoading={saving || testing}
				onSubmit={() => (connectionVerified ? handleSubmit() : handleTest())}
				submitLabel={connectionVerified
					? destination
						? m.common_save_changes()
						: m.s3_destination_add_title()
					: m.test_connection()}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>

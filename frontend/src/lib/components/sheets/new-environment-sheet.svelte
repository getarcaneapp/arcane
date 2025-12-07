<script lang="ts">
	import { toast } from 'svelte-sonner';
	import * as Sheet from '$lib/components/ui/sheet/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import UrlInput from '$lib/components/form/url-input.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import ServerIcon from 'phosphor-svelte/lib/HardDrives';
	import * as Card from '$lib/components/ui/card';
	import type { CreateEnvironmentDTO } from '$lib/types/environment.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';

	type NewEnvironmentSheetProps = {
		open: boolean;
		onEnvironmentCreated?: () => void;
	};

	let { open = $bindable(false), onEnvironmentCreated }: NewEnvironmentSheetProps = $props();

	let isSubmitting = $state(false);

	let urlProtocol = $state<'https' | 'http'>('https');
	let urlHost = $state('');

	$effect(() => {
		if (open) {
			urlProtocol = 'https';
			urlHost = '';
			formData.name = '';
			formData.apiUrl = '';
			formData.bootstrapToken = '';
		}
	});

	const formSchema = z.object({
		name: z.string().min(1, m.environments_name_required()).max(25, m.environments_name_too_long()),
		apiUrl: z.string().min(1, m.environments_server_url_required()),
		bootstrapToken: z.string()
	});

	let formData = $state({
		name: '',
		apiUrl: '',
		bootstrapToken: ''
	});

	$effect(() => {
		formData.apiUrl = urlHost;
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	async function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		try {
			isSubmitting = true;

			// Construct full URL from protocol and host
			const fullUrl = `${urlProtocol}://${urlHost}`;

			const dto: CreateEnvironmentDTO = {
				name: data.name,
				apiUrl: fullUrl,
				bootstrapToken: data.bootstrapToken
			};

			const created = await environmentManagementService.create(dto);

			try {
				const result = await environmentManagementService.testConnection(created.id);
				if (result.status === 'online') {
					toast.success(m.environments_test_connection_success());
				} else {
					toast.warning(m.environments_test_connection_error());
				}
			} catch (e) {
				console.error(e);
				toast.error(m.environments_test_connection_failed());
			}

			toast.success(m.environments_created_success());

			form.reset();
			onEnvironmentCreated?.();
		} catch (error) {
			toast.error(m.environments_create_failed());
			console.error(error);
		} finally {
			isSubmitting = false;
		}
	}
</script>

<Sheet.Root bind:open>
	<Sheet.Content class="w-full p-6 sm:max-w-lg">
		<Sheet.Header class="space-y-3 border-b pb-6">
			<div class="flex items-center gap-3">
				<div class="bg-primary/10 flex size-10 shrink-0 items-center justify-center rounded-lg">
					<ServerIcon class="text-primary size-5" />
				</div>
				<div>
					<Sheet.Title class="text-xl font-semibold">{m.environments_title()}</Sheet.Title>
					<Sheet.Description class="text-muted-foreground mt-1 text-sm">{m.environments_manage_description()}</Sheet.Description>
				</div>
			</div>
		</Sheet.Header>

		<div class="space-y-6 py-6">
			<Card.Root class="flex flex-col gap-6 py-3">
				<Card.Header
					class="@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6"
				>
					<Card.Title class="text-lg">{m.common_add_button({ resource: m.resource_environment_cap() })}</Card.Title>
				</Card.Header>
				<Card.Content class="px-6">
					<form onsubmit={preventDefault(handleSubmit)} class="space-y-4">
						<FormInput label={m.common_name()} placeholder={m.environments_name_placeholder()} bind:input={$inputs.name} />

						<UrlInput
							id="environment-api-url"
							label={m.environments_api_url()}
							placeholder={m.environments_api_url_placeholder()}
							description={m.environments_api_url_description()}
							bind:value={urlHost}
							bind:protocol={urlProtocol}
							disabled={isSubmitting}
							required
							error={$inputs.apiUrl.error ?? undefined}
						/>

						<FormInput
							label={m.environments_bootstrap_label()}
							type="password"
							placeholder={m.environments_bootstrap_placeholder()}
							description={m.environments_pair_rotate_description()}
							bind:input={$inputs.bootstrapToken}
						/>

						<Button type="submit" class="w-full" disabled={isSubmitting}>
							{#if isSubmitting}
								<Spinner class="mr-2 size-4" />
							{/if}
							{m.common_add_button({ resource: m.resource_environment_cap() })}
						</Button>
					</form>
				</Card.Content>
			</Card.Root>
		</div>
	</Sheet.Content>
</Sheet.Root>

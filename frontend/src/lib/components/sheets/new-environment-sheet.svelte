<script lang="ts">
	import { toast } from 'svelte-sonner';
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import UrlInput from '$lib/components/form/url-input.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
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

	let createdEnvironment = $state<{
		id: string;
		apiKey: string;
		name: string;
		apiUrl: string;
		dockerRun?: string;
		dockerCompose?: string;
	} | null>(null);

	let isSubmittingNewAgent = $state(false);
	let isLoadingSnippets = $state(false);

	let newAgentUrlProtocol = $state<'https' | 'http'>('http');
	let newAgentUrlHost = $state('');

	const newAgentFormSchema = z.object({
		name: z.string().min(1, m.environments_name_required()).max(25, m.environments_name_too_long()),
		apiUrl: z.string().min(1, m.environments_server_url_required())
	});

	const { inputs: newAgentInputs, ...newAgentForm } = createForm<typeof newAgentFormSchema>(newAgentFormSchema, {
		name: '',
		apiUrl: ''
	});

	// Reset on open/close
	$effect(() => {
		if (open) {
			createdEnvironment = null;
			newAgentUrlProtocol = 'http';
			newAgentUrlHost = '';
			$newAgentInputs.name.value = '';
			$newAgentInputs.apiUrl.value = '';
		}
	});

	// Sync UrlInput value with form validation
	$effect(() => {
		$newAgentInputs.apiUrl.value = newAgentUrlHost;
	});

	async function handleNewAgentSubmit() {
		const data = newAgentForm.validate();
		if (!data) return;

		try {
			isSubmittingNewAgent = true;
			const fullUrl = `${newAgentUrlProtocol}://${newAgentUrlHost}`;

			const dto: CreateEnvironmentDTO = {
				name: data.name,
				apiUrl: fullUrl,
				useApiKey: true
			};

			const created = await environmentManagementService.create(dto);

			if (created.apiKey) {
				createdEnvironment = {
					id: created.id,
					apiKey: created.apiKey,
					name: created.name,
					apiUrl: fullUrl
				};

				// Fetch deployment snippets from backend
				isLoadingSnippets = true;
				try {
					const snippets = await environmentManagementService.getDeploymentSnippets(created.id);
					createdEnvironment.dockerRun = snippets.dockerRun;
					createdEnvironment.dockerCompose = snippets.dockerCompose;
				} catch (err) {
					console.error('Failed to fetch deployment snippets:', err);
				} finally {
					isLoadingSnippets = false;
				}

				toast.success(m.environments_created_success());
			} else {
				toast.error('Failed to generate API key');
			}
		} catch (error) {
			toast.error(m.environments_create_failed());
			console.error(error);
		} finally {
			isSubmittingNewAgent = false;
		}
	}

	function handleDone() {
		onEnvironmentCreated?.();
		open = false;
	}
</script>

<ResponsiveDialog.Root
	bind:open
	variant="sheet"
	title={createdEnvironment ? m.environments_created_title() : m.environments_create_new_agent()}
	description={createdEnvironment ? m.environments_created_description() : m.environments_create_new_agent_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="space-y-6 px-6 py-6">
			{#if createdEnvironment}
				<div class="space-y-4">
					<div class="space-y-2">
						<div class="text-sm font-medium">{m.environments_api_key()}</div>
						<div class="flex items-center gap-2">
							<code class="bg-muted flex-1 rounded-md px-3 py-2 font-mono text-sm break-all">
								{createdEnvironment.apiKey}
							</code>
							{#if createdEnvironment.apiKey}
								<CopyButton text={createdEnvironment.apiKey} size="icon" class="size-7" />
							{/if}
						</div>
						<p class="text-muted-foreground text-xs">{m.environments_api_key_warning()}</p>
					</div>

					{#if isLoadingSnippets}
						<div class="flex items-center justify-center py-8">
							<Spinner class="size-6" />
						</div>
					{:else if createdEnvironment.dockerRun && createdEnvironment.dockerCompose}
						<div class="space-y-2">
							<div class="text-sm font-medium">{m.environments_docker_run_command()}</div>
							<div class="relative">
								<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs"><code>{createdEnvironment.dockerRun}</code></pre>
								<div class="absolute top-2 right-2">
									<CopyButton text={createdEnvironment.dockerRun} size="icon" class="size-7" />
								</div>
							</div>
						</div>

						<div class="space-y-2">
							<div class="text-sm font-medium">{m.environments_docker_compose()}</div>
							<div class="relative">
								<pre class="bg-muted overflow-x-auto rounded-md p-3 text-xs"><code>{createdEnvironment.dockerCompose}</code></pre>
								<div class="absolute top-2 right-2">
									<CopyButton text={createdEnvironment.dockerCompose} size="icon" class="size-7" />
								</div>
							</div>
						</div>
					{/if}

					<ArcaneButton action="base" class="w-full" onclick={handleDone} customLabel={m.common_done()} />
				</div>
			{:else}
				<form onsubmit={preventDefault(handleNewAgentSubmit)} class="space-y-4">
					<FormInput label={m.common_name()} placeholder={m.environments_production_docker()} bind:input={$newAgentInputs.name} />

					<UrlInput
						id="new-agent-api-url"
						label={m.environments_agent_address()}
						placeholder={m.environments_agent_address_placeholder()}
						description={m.environments_agent_address_description()}
						bind:value={newAgentUrlHost}
						bind:protocol={newAgentUrlProtocol}
						disabled={isSubmittingNewAgent}
						required
						error={$newAgentInputs.apiUrl.error ?? undefined}
					/>

					<ArcaneButton
						action="confirm"
						type="submit"
						class="w-full"
						disabled={isSubmittingNewAgent}
						loading={isSubmittingNewAgent}
						customLabel={m.environments_generate_config()}
					/>
				</form>
			{/if}
		</div>
	{/snippet}
</ResponsiveDialog.Root>

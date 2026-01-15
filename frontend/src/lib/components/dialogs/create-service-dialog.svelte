<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, TrashIcon } from '$lib/icons';
	import * as Accordion from '$lib/components/ui/accordion/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';

	type CreateServiceDialogProps = {
		open: boolean;
		onSubmit: (spec: Record<string, unknown>) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: CreateServiceDialogProps = $props();

	const formSchema = z.object({
		name: z.string().min(1, m.common_name_required()),
		image: z.string().min(1, m.swarm_service_form_image_required()),
		replicas: z.coerce.number().int().min(0).default(1)
	});

	const formData = {
		name: '',
		image: '',
		replicas: 1
	};

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));
	let mode = $state<'replicated' | 'global'>('replicated');
	let command = $state({ value: '', error: '' });
	let args = $state({ value: '', error: '' });
	let workingDir = $state({ value: '', error: '' });
	let user = $state({ value: '', error: '' });
	let hostname = $state({ value: '', error: '' });
	let ports = $state<Array<{ target: string; published: string; protocol: 'tcp' | 'udp' }>>([]);
	let envVars = $state<Array<{ key: string; value: string }>>([]);
	let mounts = $state<Array<{ type: 'volume' | 'bind'; source: string; target: string }>>([]);
	let labels = $state<Array<{ key: string; value: string }>>([]);

	function addPort() {
		ports = [...ports, { target: '', published: '', protocol: 'tcp' }];
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
	}

	function addEnvVar() {
		envVars = [...envVars, { key: '', value: '' }];
	}

	function removeEnvVar(index: number) {
		envVars = envVars.filter((_, i) => i !== index);
	}

	function addMount() {
		mounts = [...mounts, { type: 'volume', source: '', target: '' }];
	}

	function removeMount(index: number) {
		mounts = mounts.filter((_, i) => i !== index);
	}

	function addLabel() {
		labels = [...labels, { key: '', value: '' }];
	}

	function removeLabel(index: number) {
		labels = labels.filter((_, i) => i !== index);
	}

	function handleSubmit() {
		const validated = form.validate();
		if (!validated) return;

		const { name, image, replicas } = validated;

		const spec: any = {
			Name: name,
			TaskTemplate: {
				ContainerSpec: {
					Image: image
				}
			},
			Mode: mode === 'replicated' ? { Replicated: { Replicas: replicas } } : { Global: {} }
		};

		// Add optional container config
		if (command.value.trim()) {
			spec.TaskTemplate.ContainerSpec.Command = command.value.trim().split(' ').filter(Boolean);
		}
		if (args.value.trim()) {
			spec.TaskTemplate.ContainerSpec.Args = args.value.trim().split(' ').filter(Boolean);
		}
		if (workingDir.value.trim()) {
			spec.TaskTemplate.ContainerSpec.Dir = workingDir.value.trim();
		}
		if (user.value.trim()) {
			spec.TaskTemplate.ContainerSpec.User = user.value.trim();
		}
		if (hostname.value.trim()) {
			spec.TaskTemplate.ContainerSpec.Hostname = hostname.value.trim();
		}

		// Add environment variables
		if (envVars.length > 0) {
			const filtered = envVars.filter((e) => e.key.trim());
			if (filtered.length > 0) {
				spec.TaskTemplate.ContainerSpec.Env = filtered.map((e) => `${e.key}=${e.value}`);
			}
		}

		// Add mounts
		if (mounts.length > 0) {
			const filtered = mounts.filter((m) => m.source.trim() && m.target.trim());
			if (filtered.length > 0) {
				spec.TaskTemplate.ContainerSpec.Mounts = filtered.map((m) => ({
					Type: m.type,
					Source: m.source,
					Target: m.target
				}));
			}
		}

		// Add labels
		if (labels.length > 0) {
			const filtered = labels.filter((l) => l.key.trim());
			if (filtered.length > 0) {
				spec.Labels = {};
				filtered.forEach((l) => {
					spec.Labels[l.key] = l.value;
				});
			}
		}

		// Add ports
		if (ports.length > 0) {
			const filtered = ports.filter((p) => p.target.trim());
			if (filtered.length > 0) {
				spec.EndpointSpec = {
					Ports: filtered.map((p) => ({
						TargetPort: parseInt(p.target),
						PublishedPort: p.published ? parseInt(p.published) : undefined,
						Protocol: p.protocol
					}))
				};
			}
		}

		onSubmit(spec);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			form.reset();
			mode = 'replicated';
			command.value = '';
			args.value = '';
			workingDir.value = '';
			user.value = '';
			hostname.value = '';
			ports = [];
			envVars = [];
			mounts = [];
			labels = [];
		}
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.swarm_service_create_title()}
	description={m.swarm_service_create_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="max-h-[70vh] space-y-6 overflow-y-auto py-4">
			<!-- Basic Config -->
			<div class="space-y-4">
				<FormInput input={$inputs.name} label={m.common_name()} placeholder="my-service" disabled={isLoading} />
				<FormInput
					input={$inputs.image}
					label={m.swarm_service_form_image_label()}
					placeholder={m.swarm_service_form_image_placeholder()}
					disabled={isLoading}
				/>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<FormInput label={m.swarm_mode()}>
					{#snippet children()}
						<Select.Root type="single" bind:value={mode} disabled={isLoading}>
							<Select.Trigger class="w-full">
								<span class="capitalize"
									>{mode === 'replicated' ? m.swarm_service_mode_replicated() : m.swarm_service_mode_global()}</span
								>
							</Select.Trigger>
							<Select.Content>
								<Select.Item value="replicated" label={m.swarm_service_mode_replicated()}
									>{m.swarm_service_mode_replicated()}</Select.Item
								>
								<Select.Item value="global" label={m.swarm_service_mode_global()}>{m.swarm_service_mode_global()}</Select.Item>
							</Select.Content>
						</Select.Root>
					{/snippet}
				</FormInput>

				{#if mode === 'replicated'}
					<FormInput input={$inputs.replicas} label={m.swarm_replicas()} type="number" disabled={isLoading} />
				{/if}
			</div>

			<Accordion.Root class="w-full space-y-2" type="multiple">
				<Accordion.Item value="ports">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_ports()}</Accordion.Trigger>
					<Accordion.Content class="space-y-4 pt-4 pb-2">
						{#each ports as port, i (i)}
							<div class="flex items-center gap-2">
								<Input placeholder="8080" bind:value={port.target} disabled={isLoading} class="flex-1" />
								<span class="text-muted-foreground">:</span>
								<Input placeholder="80" bind:value={port.published} disabled={isLoading} class="flex-1" />
								<Select.Root type="single" bind:value={port.protocol} disabled={isLoading}>
									<Select.Trigger class="w-20">
										<span class="uppercase">{port.protocol}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="tcp" label="TCP">TCP</Select.Item>
										<Select.Item value="udp" label="UDP">UDP</Select.Item>
									</Select.Content>
								</Select.Root>
								<ArcaneButton action="remove" size="sm" onclick={() => removePort(i)} disabled={isLoading} icon={TrashIcon} />
							</div>
						{/each}
						<ArcaneButton
							action="create"
							size="sm"
							onclick={addPort}
							disabled={isLoading}
							icon={AddIcon}
							customLabel={m.swarm_service_form_add_port()}
						/>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="env">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_env_vars()}</Accordion.Trigger>
					<Accordion.Content class="space-y-4 pt-4 pb-2">
						{#each envVars as env, i (i)}
							<div class="flex items-center gap-2">
								<Input placeholder="KEY" bind:value={env.key} disabled={isLoading} class="flex-1" />
								<span class="text-muted-foreground">=</span>
								<Input placeholder="value" bind:value={env.value} disabled={isLoading} class="flex-1" />
								<ArcaneButton action="remove" size="sm" onclick={() => removeEnvVar(i)} disabled={isLoading} icon={TrashIcon} />
							</div>
						{/each}
						<ArcaneButton
							action="create"
							size="sm"
							onclick={addEnvVar}
							disabled={isLoading}
							icon={AddIcon}
							customLabel={m.swarm_service_form_add_variable()}
						/>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="mounts">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_mounts()}</Accordion.Trigger>
					<Accordion.Content class="space-y-4 pt-4 pb-2">
						{#each mounts as mount, i (i)}
							<div class="flex items-center gap-2">
								<Select.Root type="single" bind:value={mount.type} disabled={isLoading}>
									<Select.Trigger class="w-24">
										<span class="capitalize">{mount.type}</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="volume" label="Volume">Volume</Select.Item>
										<Select.Item value="bind" label="Bind">Bind</Select.Item>
									</Select.Content>
								</Select.Root>
								<Input placeholder="source" bind:value={mount.source} disabled={isLoading} class="flex-1" />
								<span class="text-muted-foreground">→</span>
								<Input placeholder="/target" bind:value={mount.target} disabled={isLoading} class="flex-1" />
								<ArcaneButton action="remove" size="sm" onclick={() => removeMount(i)} disabled={isLoading} icon={TrashIcon} />
							</div>
						{/each}
						<ArcaneButton
							action="create"
							size="sm"
							onclick={addMount}
							disabled={isLoading}
							icon={AddIcon}
							customLabel={m.swarm_service_form_add_mount()}
						/>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="labels">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_labels()}</Accordion.Trigger>
					<Accordion.Content class="space-y-4 pt-4 pb-2">
						{#each labels as label, i (i)}
							<div class="flex items-center gap-2">
								<Input placeholder="key" bind:value={label.key} disabled={isLoading} class="flex-1" />
								<span class="text-muted-foreground">=</span>
								<Input placeholder="value" bind:value={label.value} disabled={isLoading} class="flex-1" />
								<ArcaneButton action="remove" size="sm" onclick={() => removeLabel(i)} disabled={isLoading} icon={TrashIcon} />
							</div>
						{/each}
						<ArcaneButton
							action="create"
							size="sm"
							onclick={addLabel}
							disabled={isLoading}
							icon={AddIcon}
							customLabel={m.swarm_service_form_add_label()}
						/>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="advanced">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_advanced()}</Accordion.Trigger>
					<Accordion.Content class="space-y-4 pt-4 pb-2">
						<FormInput input={command} label={m.swarm_service_form_command()} placeholder="/bin/sh" disabled={isLoading} />
						<FormInput input={args} label={m.swarm_service_form_arguments()} placeholder="-c echo hello" disabled={isLoading} />
						<FormInput input={workingDir} label={m.swarm_service_form_working_dir()} placeholder="/app" disabled={isLoading} />
						<FormInput input={user} label={m.swarm_service_form_user()} placeholder="1000:1000" disabled={isLoading} />
						<FormInput input={hostname} label={m.swarm_hostname()} placeholder="my-service" disabled={isLoading} />
					</Accordion.Content>
				</Accordion.Item>
			</Accordion.Root>
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="ghost"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			<ArcaneButton
				action="create"
				type="button"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>

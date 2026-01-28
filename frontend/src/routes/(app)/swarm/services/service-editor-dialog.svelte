<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, TrashIcon } from '$lib/icons';
	import * as Accordion from '$lib/components/ui/accordion/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';

	type ServiceEditorPayload = {
		spec: Record<string, unknown>;
		options?: Record<string, unknown>;
	};

	type ServiceEditorDialogProps = {
		open: boolean;
		title: string;
		description: string;
		submitLabel: string;
		submitAction?: 'save' | 'create' | 'update';
		initialSpec: string;
		initialOptions?: string;
		isLoading: boolean;
		onSubmit: (payload: ServiceEditorPayload) => void;
	};

	let {
		open = $bindable(false),
		title,
		description,
		submitLabel,
		submitAction = 'save',
		initialSpec,
		initialOptions = '',
		isLoading,
		onSubmit
	}: ServiceEditorDialogProps = $props();

	// Form state
	let name = $state({ value: '', error: '' });
	let image = $state({ value: '', error: '' });
	let mode = $state<'replicated' | 'global'>('replicated');
	let replicas = $state({ value: '1', error: '' });
	let command = $state({ value: '', error: '' });
	let args = $state({ value: '', error: '' });
	let workingDir = $state({ value: '', error: '' });
	let user = $state({ value: '', error: '' });
	let hostname = $state({ value: '', error: '' });
	let ports = $state<Array<{ target: string; published: string; protocol: 'tcp' | 'udp' }>>([]);
	let envVars = $state<Array<{ key: string; value: string }>>([]);
	let mounts = $state<Array<{ type: 'volume' | 'bind'; source: string; target: string }>>([]);
	let labels = $state<Array<{ key: string; value: string }>>([]);

	// Parse initial spec when dialog opens
	$effect(() => {
		if (open && initialSpec) {
			try {
				const spec = JSON.parse(initialSpec);

				name.value = spec.Name || '';
				image.value = spec.TaskTemplate?.ContainerSpec?.Image || '';

				// Parse mode
				if (spec.Mode?.Replicated) {
					mode = 'replicated';
					replicas.value = String(spec.Mode.Replicated.Replicas || 1);
				} else if (spec.Mode?.Global) {
					mode = 'global';
				}

				// Parse container config
				const containerSpec = spec.TaskTemplate?.ContainerSpec || {};
				command.value = containerSpec.Command ? containerSpec.Command.join(' ') : '';
				args.value = containerSpec.Args ? containerSpec.Args.join(' ') : '';
				workingDir.value = containerSpec.Dir || '';
				user.value = containerSpec.User || '';
				hostname.value = containerSpec.Hostname || '';

				// Parse env vars
				envVars =
					containerSpec.Env?.map((env: string) => {
						const [key, ...valueParts] = env.split('=');
						return { key, value: valueParts.join('=') };
					}) || [];

				// Parse mounts
				mounts =
					containerSpec.Mounts?.map((m: any) => ({
						type: m.Type || 'volume',
						source: m.Source || '',
						target: m.Target || ''
					})) || [];

				// Parse labels
				labels = spec.Labels
					? Object.entries(spec.Labels).map(([key, value]) => ({
							key,
							value: String(value)
						}))
					: [];

				// Parse ports
				ports =
					spec.EndpointSpec?.Ports?.map((p: any) => ({
						target: String(p.TargetPort || ''),
						published: p.PublishedPort ? String(p.PublishedPort) : '',
						protocol: p.Protocol || 'tcp'
					})) || [];
			} catch (e) {
				console.error('Failed to parse service spec:', e);
			}
		}
	});

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
		// Validation
		if (!name.value.trim()) {
			name.error = m.common_name_required();
			return;
		}
		if (!image.value.trim()) {
			image.error = m.swarm_service_form_image_required();
			return;
		}

		const spec: any = {
			Name: name.value,
			TaskTemplate: {
				ContainerSpec: {
					Image: image.value
				}
			},
			Mode: mode === 'replicated' ? { Replicated: { Replicas: parseInt(replicas.value) || 1 } } : { Global: {} }
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

		onSubmit({ spec });
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
	}
</script>

<ResponsiveDialog bind:open onOpenChange={handleOpenChange} variant="sheet" {title} {description} contentClass="sm:max-w-[600px]">
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="max-h-[70vh] space-y-6 overflow-y-auto py-4">
			<!-- Basic Config -->
			<div class="space-y-4">
				<FormInput input={name} label={m.common_name()} placeholder="my-service" disabled={isLoading} />
				<FormInput
					input={image}
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
					<FormInput input={replicas} label={m.swarm_replicas()} type="number" disabled={isLoading} />
				{/if}
			</div>

			<!-- Advanced Options -->
			<Accordion.Root class="w-full space-y-2" type="multiple">
				<!-- Ports -->
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

				<!-- Environment Variables -->
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

				<!-- Mounts -->
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

				<!-- Labels -->
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

				<!-- Advanced Container Config -->
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
				action={submitAction}
				type="button"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={submitLabel}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>

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
	import { untrack } from 'svelte';
	import { getSwarmServiceModeLabel } from '$lib/utils/swarm-service-mode.utils';

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
		isLoading: boolean;
		onSubmit: (payload: ServiceEditorPayload) => void;
	};

	type TextFieldState = {
		value: string;
		error: string;
	};

	type ServiceEditorMode = 'replicated' | 'global' | 'replicated-job' | 'global-job';

	type ServicePortDraft = {
		target: string;
		published: string;
		protocol: 'tcp' | 'udp';
	};

	type ServiceEnvDraft = {
		key: string;
		value: string;
	};

	type ServiceMountDraft = {
		type: 'volume' | 'bind';
		source: string;
		target: string;
	};

	type ServiceLabelDraft = {
		key: string;
		value: string;
	};

	type SwarmServiceMountRaw = {
		Type?: string;
		Source?: string;
		Target?: string;
	};

	type SwarmServicePortRaw = {
		TargetPort?: number;
		PublishedPort?: number;
		Protocol?: string;
	};

	type SwarmServiceSpecRaw = {
		Name?: string;
		Mode?: {
			Replicated?: {
				Replicas?: number;
			};
			Global?: Record<string, unknown>;
			ReplicatedJob?: {
				MaxConcurrent?: number;
				TotalCompletions?: number;
			};
			GlobalJob?: Record<string, unknown>;
		};
		TaskTemplate?: {
			ContainerSpec?: {
				Image?: string;
				Command?: string[];
				Args?: string[];
				Dir?: string;
				User?: string;
				Hostname?: string;
				Env?: string[];
				Mounts?: SwarmServiceMountRaw[];
			};
		};
		Labels?: Record<string, string>;
		EndpointSpec?: {
			Ports?: SwarmServicePortRaw[];
		};
	};

	type ServiceEditorFormState = {
		name: TextFieldState;
		image: TextFieldState;
		mode: ServiceEditorMode;
		replicas: TextFieldState;
		command: TextFieldState;
		args: TextFieldState;
		workingDir: TextFieldState;
		user: TextFieldState;
		hostname: TextFieldState;
		ports: ServicePortDraft[];
		envVars: ServiceEnvDraft[];
		mounts: ServiceMountDraft[];
		labels: ServiceLabelDraft[];
		baseSpec: Record<string, unknown>;
	};

	let {
		open = $bindable(false),
		title,
		description,
		submitLabel,
		submitAction = 'save',
		initialSpec,
		isLoading,
		onSubmit
	}: ServiceEditorDialogProps = $props();

	function asRecord(value: unknown): Record<string, unknown> | undefined {
		return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
	}

	function createTextField(value = ''): TextFieldState {
		return { value, error: '' };
	}

	function createEmptyFormState(): ServiceEditorFormState {
		return {
			name: createTextField(),
			image: createTextField(),
			mode: 'replicated',
			replicas: createTextField('1'),
			command: createTextField(),
			args: createTextField(),
			workingDir: createTextField(),
			user: createTextField(),
			hostname: createTextField(),
			ports: [],
			envVars: [],
			mounts: [],
			labels: [],
			baseSpec: {}
		};
	}

	function normalizeMountType(value: string | undefined): 'volume' | 'bind' {
		return value === 'bind' ? 'bind' : 'volume';
	}

	function normalizePortProtocol(value: string | undefined): 'tcp' | 'udp' {
		return value === 'udp' ? 'udp' : 'tcp';
	}

	function isJobMode(mode: ServiceEditorMode): boolean {
		return mode === 'replicated-job' || mode === 'global-job';
	}

	function parseInitialSpec(specText: string): SwarmServiceSpecRaw | null {
		if (!specText.trim()) return null;

		try {
			const parsed = JSON.parse(specText) as unknown;
			return asRecord(parsed) as SwarmServiceSpecRaw;
		} catch (error) {
			console.error('Failed to parse service spec:', error);
			return null;
		}
	}

	function createFormState(spec: SwarmServiceSpecRaw | null): ServiceEditorFormState {
		if (!spec) return createEmptyFormState();

		const containerSpec = spec.TaskTemplate?.ContainerSpec;
		let mode: ServiceEditorMode = 'replicated';
		if (spec.Mode?.Global) mode = 'global';
		if (spec.Mode?.ReplicatedJob) mode = 'replicated-job';
		if (spec.Mode?.GlobalJob) mode = 'global-job';

		return {
			name: createTextField(spec.Name ?? ''),
			image: createTextField(containerSpec?.Image ?? ''),
			mode,
			replicas: createTextField(String(spec.Mode?.Replicated?.Replicas ?? 1)),
			command: createTextField(containerSpec?.Command?.join(' ') ?? ''),
			args: createTextField(containerSpec?.Args?.join(' ') ?? ''),
			workingDir: createTextField(containerSpec?.Dir ?? ''),
			user: createTextField(containerSpec?.User ?? ''),
			hostname: createTextField(containerSpec?.Hostname ?? ''),
			ports:
				spec.EndpointSpec?.Ports?.map((port) => ({
					target: String(port.TargetPort ?? ''),
					published: port.PublishedPort ? String(port.PublishedPort) : '',
					protocol: normalizePortProtocol(port.Protocol)
				})) ?? [],
			envVars:
				containerSpec?.Env?.map((envEntry) => {
					const [key, ...valueParts] = envEntry.split('=');
					return { key, value: valueParts.join('=') };
				}) ?? [],
			mounts:
				containerSpec?.Mounts?.map((mount) => ({
					type: normalizeMountType(mount.Type),
					source: mount.Source ?? '',
					target: mount.Target ?? ''
				})) ?? [],
			labels: spec.Labels ? Object.entries(spec.Labels).map(([key, value]) => ({ key, value: String(value) })) : [],
			baseSpec: structuredClone(spec as Record<string, unknown>)
		};
	}

	let form = $state<ServiceEditorFormState>(untrack(() => createFormState(parseInitialSpec(initialSpec))));

	function ensureRecord(parent: Record<string, unknown>, key: string): Record<string, unknown> {
		const existing = asRecord(parent[key]);
		if (existing) return existing;
		const created: Record<string, unknown> = {};
		parent[key] = created;
		return created;
	}

	function assignOptional(target: Record<string, unknown>, key: string, value: string) {
		const trimmed = value.trim();
		if (trimmed) {
			target[key] = trimmed;
			return;
		}
		delete target[key];
	}

	function addPort() {
		form.ports = [...form.ports, { target: '', published: '', protocol: 'tcp' }];
	}

	function removePort(index: number) {
		form.ports = form.ports.filter((_, i) => i !== index);
	}

	function addEnvVar() {
		form.envVars = [...form.envVars, { key: '', value: '' }];
	}

	function removeEnvVar(index: number) {
		form.envVars = form.envVars.filter((_, i) => i !== index);
	}

	function addMount() {
		form.mounts = [...form.mounts, { type: 'volume', source: '', target: '' }];
	}

	function removeMount(index: number) {
		form.mounts = form.mounts.filter((_, i) => i !== index);
	}

	function addLabel() {
		form.labels = [...form.labels, { key: '', value: '' }];
	}

	function removeLabel(index: number) {
		form.labels = form.labels.filter((_, i) => i !== index);
	}

	function handleSubmit() {
		// Validation
		if (!form.name.value.trim()) {
			form.name.error = m.common_name_required();
			return;
		}
		if (!form.image.value.trim()) {
			form.image.error = m.swarm_service_form_image_required();
			return;
		}

		const spec = structuredClone(form.baseSpec ?? {});
		spec.Name = form.name.value.trim();

		const taskTemplate = ensureRecord(spec, 'TaskTemplate');
		const containerSpec = ensureRecord(taskTemplate, 'ContainerSpec');
		containerSpec.Image = form.image.value.trim();

		const baseMode = asRecord(form.baseSpec.Mode);
		if (form.mode === 'replicated') {
			spec.Mode = { Replicated: { Replicas: Math.max(0, parseInt(form.replicas.value, 10) || 1) } };
		} else if (form.mode === 'global') {
			spec.Mode = { Global: {} };
		} else if (form.mode === 'replicated-job') {
			spec.Mode = { ReplicatedJob: structuredClone(asRecord(baseMode?.ReplicatedJob) ?? {}) };
		} else {
			spec.Mode = { GlobalJob: structuredClone(asRecord(baseMode?.GlobalJob) ?? {}) };
		}

		// Add optional container config
		const commandParts = form.command.value.trim().split(' ').filter(Boolean);
		if (commandParts.length > 0) {
			containerSpec.Command = commandParts;
		} else {
			delete containerSpec.Command;
		}

		const argParts = form.args.value.trim().split(' ').filter(Boolean);
		if (argParts.length > 0) {
			containerSpec.Args = argParts;
		} else {
			delete containerSpec.Args;
		}

		assignOptional(containerSpec, 'Dir', form.workingDir.value);
		assignOptional(containerSpec, 'User', form.user.value);
		assignOptional(containerSpec, 'Hostname', form.hostname.value);

		// Add environment variables
		const filteredEnvVars = form.envVars.filter((entry) => entry.key.trim());
		if (filteredEnvVars.length > 0) {
			containerSpec.Env = filteredEnvVars.map((entry) => `${entry.key}=${entry.value}`);
		} else {
			delete containerSpec.Env;
		}

		// Add mounts
		const filteredMounts = form.mounts.filter((mount) => mount.source.trim() && mount.target.trim());
		if (filteredMounts.length > 0) {
			containerSpec.Mounts = filteredMounts.map((mount) => ({
				Type: mount.type,
				Source: mount.source,
				Target: mount.target
			}));
		} else {
			delete containerSpec.Mounts;
		}

		// Add labels
		const filteredLabels = form.labels.filter((label) => label.key.trim());
		if (filteredLabels.length > 0) {
			const nextLabels: Record<string, string> = {};
			filteredLabels.forEach((label) => {
				nextLabels[label.key] = label.value;
			});
			spec.Labels = nextLabels;
		} else {
			delete spec.Labels;
		}

		// Add ports
		const filteredPorts = form.ports.filter((port) => port.target.trim());
		if (filteredPorts.length > 0) {
			const endpointSpec = ensureRecord(spec, 'EndpointSpec');
			endpointSpec.Ports = filteredPorts.map((port) => ({
				TargetPort: parseInt(port.target, 10),
				PublishedPort: port.published ? parseInt(port.published, 10) : undefined,
				Protocol: port.protocol
			}));
		} else {
			const endpointSpec = asRecord(spec.EndpointSpec);
			if (endpointSpec) {
				delete endpointSpec.Ports;
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
		<form onsubmit={preventDefault(handleSubmit)} class="space-y-6 py-4">
			<!-- Basic Config -->
			<div class="space-y-4">
				<FormInput input={form.name} label={m.common_name()} placeholder="my-service" disabled={isLoading} />
				<FormInput
					input={form.image}
					label={m.swarm_service_form_image_label()}
					placeholder={m.swarm_service_form_image_placeholder()}
					disabled={isLoading}
				/>
			</div>

			<div class="grid grid-cols-2 gap-4">
				<FormInput label={m.swarm_mode()}>
					{#snippet children()}
						<Select.Root type="single" bind:value={form.mode} disabled={isLoading || isJobMode(form.mode)}>
							<Select.Trigger class="w-full">
								<span>{getSwarmServiceModeLabel(form.mode)}</span>
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

				{#if form.mode === 'replicated'}
					<FormInput input={form.replicas} label={m.swarm_replicas()} type="number" disabled={isLoading} />
				{/if}
			</div>

			<!-- Advanced Options -->
			<Accordion.Root class="w-full space-y-5" type="multiple">
				<!-- Ports -->
				<Accordion.Item value="ports">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_ports()}</Accordion.Trigger>
					<Accordion.Content class="pt-6 pb-5">
						<div class="space-y-4">
							{#each form.ports as port, i (i)}
								<div class="flex items-center gap-4">
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
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<!-- Environment Variables -->
				<Accordion.Item value="env">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_env_vars()}</Accordion.Trigger>
					<Accordion.Content class="pt-6 pb-5">
						<div class="space-y-4">
							{#each form.envVars as env, i (i)}
								<div class="flex items-center gap-4">
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
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<!-- Mounts -->
				<Accordion.Item value="mounts">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_mounts()}</Accordion.Trigger>
					<Accordion.Content class="pt-6 pb-5">
						<div class="space-y-4">
							{#each form.mounts as mount, i (i)}
								<div class="flex items-center gap-4">
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
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<!-- Labels -->
				<Accordion.Item value="labels">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_labels()}</Accordion.Trigger>
					<Accordion.Content class="pt-6 pb-5">
						<div class="space-y-4">
							{#each form.labels as label, i (i)}
								<div class="flex items-center gap-4">
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
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<!-- Advanced Container Config -->
				<Accordion.Item value="advanced">
					<Accordion.Trigger class="text-sm font-medium">{m.swarm_service_form_advanced()}</Accordion.Trigger>
					<Accordion.Content class="pt-6 pb-5">
						<div class="space-y-5">
							<FormInput input={form.command} label={m.swarm_service_form_command()} placeholder="/bin/sh" disabled={isLoading} />
							<FormInput
								input={form.args}
								label={m.swarm_service_form_arguments()}
								placeholder="-c echo hello"
								disabled={isLoading}
							/>
							<FormInput
								input={form.workingDir}
								label={m.swarm_service_form_working_dir()}
								placeholder="/app"
								disabled={isLoading}
							/>
							<FormInput input={form.user} label={m.swarm_service_form_user()} placeholder="1000:1000" disabled={isLoading} />
							<FormInput input={form.hostname} label={m.swarm_hostname()} placeholder="my-service" disabled={isLoading} />
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

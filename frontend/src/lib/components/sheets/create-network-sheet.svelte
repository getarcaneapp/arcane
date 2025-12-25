<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Accordion from '$lib/components/ui/accordion/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { NetworkCreateOptions } from '$lib/types/network.type';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import SelectWithLabel from '../form/select-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, CloseIcon, NetworksIcon } from '$lib/icons';

	type CreateNetworkFormProps = {
		open: boolean;
		onSubmit: (name: string, options: NetworkCreateOptions) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), onSubmit, isLoading }: CreateNetworkFormProps = $props();

	const drivers = [
		{ value: 'bridge', label: m.bridge() },
		{ value: 'overlay', label: m.networks_overlay() },
		{ value: 'macvlan', label: m.networks_macvlan() },
		{ value: 'ipvlan', label: m.networks_ipvlan() },
		{ value: 'none', label: m.networks_none() }
	];

	const formSchema = z.object({
		networkName: z.string().min(1, m.network_name_required()),
		networkDriver: z.string().min(1, m.common_driver_required()),
		checkDuplicate: z.boolean().default(true),
		internal: z.boolean().default(false),
		networkLabels: z.string().optional().default(''),
		driverOptions: z.string().optional().default(''),
		enableIpam: z.boolean().default(false),
		subnet: z.string().optional().default(''),
		gateway: z.string().optional().default('')
	});

	let formData = $derived({
		networkName: '',
		networkDriver: 'bridge',
		checkDuplicate: true,
		internal: false,
		networkLabels: '',
		driverOptions: '',
		enableIpam: false,
		subnet: '',
		gateway: ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	// Dynamic labels state for the key-value pairs
	let labels = $state<{ key: string; value: string }[]>([{ key: '', value: '' }]);

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

	function addLabel() {
		labels = [...labels, { key: '', value: '' }];
	}

	function removeLabel(index: number) {
		labels = labels.filter((_, i) => i !== index);
	}

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		// Combine textarea labels with dynamic labels
		const textareaLabels = parseKeyValuePairs(data.networkLabels || '');
		const dynamicLabels: Record<string, string> = {};

		labels.forEach((label) => {
			if (label.key.trim()) {
				dynamicLabels[label.key.trim()] = label.value.trim();
			}
		});

		const finalLabels = { ...textareaLabels, ...dynamicLabels };

		// Parse driver options (e.g., parent=eth0 for macvlan)
		const driverOptions = parseKeyValuePairs(data.driverOptions || '');

		const options: NetworkCreateOptions = {
			driver: data.networkDriver,
			checkDuplicate: data.checkDuplicate,
			internal: data.internal,
			labels: Object.keys(finalLabels).length > 0 ? finalLabels : undefined,
			options: Object.keys(driverOptions).length > 0 ? driverOptions : undefined
		};

		// Add IPAM configuration if enabled
		if (data.enableIpam && (data.subnet?.trim() || data.gateway?.trim())) {
			const ipamConfig: { subnet?: string; gateway?: string } = {};

			if (data.subnet?.trim()) {
				ipamConfig.subnet = data.subnet.trim();
			}
			if (data.gateway?.trim()) {
				ipamConfig.gateway = data.gateway.trim();
			}

			if (Object.keys(ipamConfig).length > 0) {
				options.ipam = {
					driver: 'default',
					config: [ipamConfig]
				};
			}
		}

		onSubmit(data.networkName.trim(), options);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			// Reset form data
			$inputs.networkName.value = '';
			$inputs.networkDriver.value = 'bridge';
			$inputs.checkDuplicate.value = true;
			$inputs.internal.value = false;
			$inputs.networkLabels.value = '';
			$inputs.driverOptions.value = '';
			$inputs.enableIpam.value = false;
			$inputs.subnet.value = '';
			$inputs.gateway.value = '';
			labels = [{ key: '', value: '' }];
		}
	}
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.create_network_title()}
	description={m.create_network_description()}
	contentClass="sm:max-w-[600px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<div class="space-y-2">
				<Label for="network-name" class="text-sm font-medium">{m.network_name_label()}</Label>
				<Input
					id="network-name"
					type="text"
					placeholder={m.network_name_placeholder()}
					disabled={isLoading}
					bind:value={$inputs.networkName.value}
					class={$inputs.networkName.error ? 'border-destructive' : ''}
				/>
				{#if $inputs.networkName.error}
					<p class="text-destructive text-xs">{$inputs.networkName.error}</p>
				{/if}
				<p class="text-muted-foreground text-xs">{m.network_name_description()}</p>
			</div>

			<SelectWithLabel
				id="driver-select"
				bind:value={$inputs.networkDriver.value}
				label={m.network_driver_label()}
				description={m.network_driver_description()}
				options={drivers}
				placeholder={m.network_driver_placeholder()}
			/>

			<div class="space-y-4">
				<div class="flex items-center space-x-4">
					<div class="flex items-center space-x-2">
						<Checkbox id="check-duplicate" bind:checked={$inputs.checkDuplicate.value} disabled={isLoading} />
						<Label for="check-duplicate" class="text-sm font-normal">{m.network_check_duplicate_label()}</Label>
					</div>
					<div class="flex items-center space-x-2">
						<Checkbox id="internal" bind:checked={$inputs.internal.value} disabled={isLoading} />
						<Label for="internal" class="text-sm font-normal">{m.network_internal_label()}</Label>
					</div>
				</div>
			</div>

			<Accordion.Root type="single" class="w-full">
				<Accordion.Item value="labels">
					<Accordion.Trigger class="text-sm font-medium">{m.common_labels()}</Accordion.Trigger>
					<Accordion.Content class="pt-4">
						<div class="space-y-4">
							<div class="space-y-2">
								<Label class="text-sm font-medium">{m.labels_key_value_label()}</Label>
								{#each labels as label, index (index)}
									<div class="flex items-center gap-2">
										<Input type="text" placeholder="Key" bind:value={label.key} disabled={isLoading} class="flex-1" />
										<Input type="text" placeholder="Value" bind:value={label.value} disabled={isLoading} class="flex-1" />
										<ArcaneButton
											action="base"
											tone="ghost"
											size="icon"
											onclick={() => removeLabel(index)}
											disabled={isLoading || labels.length <= 1}
											class="text-destructive hover:text-destructive"
											title={m.common_remove()}
											icon={CloseIcon}
										/>
									</div>
								{/each}
								<ArcaneButton
									action="base"
									tone="outline"
									size="sm"
									onclick={addLabel}
									disabled={isLoading}
									customLabel={m.add_label_button()}
								/>
							</div>

							<div class="space-y-2">
								<Label for="network-labels" class="text-sm font-medium">{m.network_labels_text_label()}</Label>
								<Textarea
									id="network-labels"
									placeholder={m.network_labels_placeholder()}
									disabled={isLoading}
									rows={3}
									bind:value={$inputs.networkLabels.value}
									class={$inputs.networkLabels.error ? 'border-destructive' : ''}
								/>
								{#if $inputs.networkLabels.error}
									<p class="text-destructive text-xs">{$inputs.networkLabels.error}</p>
								{/if}
								<p class="text-muted-foreground text-xs">{m.network_labels_description()}</p>
							</div>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="options">
					<Accordion.Trigger class="text-sm font-medium">{m.common_driver_options()}</Accordion.Trigger>
					<Accordion.Content class="pt-4">
						<div class="space-y-2">
							<Label for="driver-options" class="text-sm font-medium">{m.common_driver_options()}</Label>
							<Textarea
								id="driver-options"
								placeholder={m.network_driver_options_placeholder()}
								disabled={isLoading}
								rows={3}
								bind:value={$inputs.driverOptions.value}
								class={$inputs.driverOptions.error ? 'border-destructive' : ''}
							/>
							{#if $inputs.driverOptions.error}
								<p class="text-destructive text-xs">{$inputs.driverOptions.error}</p>
							{/if}
							<p class="text-muted-foreground text-xs">{m.network_driver_options_description()}</p>
						</div>
					</Accordion.Content>
				</Accordion.Item>

				<Accordion.Item value="ipam">
					<Accordion.Trigger class="text-sm font-medium">{m.networks_ipam_title()}</Accordion.Trigger>
					<Accordion.Content class="pt-4">
						<div class="space-y-4">
							<div class="flex items-center space-x-2">
								<Checkbox id="enable-ipam" bind:checked={$inputs.enableIpam.value} disabled={isLoading} />
								<Label for="enable-ipam" class="text-sm font-medium">{m.network_enable_ipam_label()}</Label>
							</div>

							{#if $inputs.enableIpam.value}
								<div class="border-muted space-y-4 border-l-2 pl-6">
									<div class="space-y-2">
										<Label for="subnet" class="text-sm font-medium">{m.common_subnet()}</Label>
										<Input
											id="subnet"
											type="text"
											placeholder="e.g., 172.20.0.0/16"
											disabled={isLoading}
											bind:value={$inputs.subnet.value}
											class={$inputs.subnet.error ? 'border-destructive' : ''}
										/>
										{#if $inputs.subnet.error}
											<p class="text-destructive text-xs">{$inputs.subnet.error}</p>
										{/if}
										<p class="text-muted-foreground text-xs">{m.network_subnet_description()}</p>
									</div>

									<div class="space-y-2">
										<Label for="gateway" class="text-sm font-medium">{m.networks_ipam_gateway_label()}</Label>
										<Input
											id="gateway"
											type="text"
											placeholder="e.g., 172.20.0.1"
											disabled={isLoading}
											bind:value={$inputs.gateway.value}
											class={$inputs.gateway.error ? 'border-destructive' : ''}
										/>
										{#if $inputs.gateway.error}
											<p class="text-destructive text-xs">{$inputs.gateway.error}</p>
										{/if}
										<p class="text-muted-foreground text-xs">{m.network_gateway_description()}</p>
									</div>
								</div>
							{/if}
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
				customLabel={m.common_create_button({ resource: m.resource_network_cap() })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>

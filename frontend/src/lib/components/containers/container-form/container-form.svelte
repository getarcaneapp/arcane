<script lang="ts">
	import { tick, onDestroy } from 'svelte';
	import { hasPermission } from '#lib/utils/auth';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import SearchableSelect from '#lib/components/form/searchable-select.svelte';
	import KeyValueEditor from '#lib/components/form/key-value-editor.svelte';
	import PortMappingEditor from '#lib/components/form/port-mapping-editor.svelte';
	import VolumeMountEditor from '#lib/components/form/volume-mount-editor.svelte';
	import NetworkAttachmentEditor from '#lib/components/form/network-attachment-editor.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages';
	import { CloseIcon, ContainersIcon, NetworksIcon, SettingsIcon, VariableIcon, VolumesIcon } from '#lib/icons';
	import { preventDefault, createForm } from '#lib/utils/settings';
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys';
	import { volumeService } from '#lib/services/volume-service';
	import { networkService } from '#lib/services/network-service';
	import { imageService } from '#lib/services/image-service';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import type { ImageSearchResultDto } from '#lib/types/docker';
	import {
		LINUX_CAPABILITIES,
		containerFormSchema,
		emptyContainerFormValues,
		type ContainerFormRows
	} from './container-form-state';

	let {
		mode,
		form,
		rows = $bindable(),
		submitting = false,
		cancelHref = '/containers',
		onSubmit
	}: {
		mode: 'create' | 'edit';
		form: ReturnType<typeof createForm<typeof containerFormSchema>>;
		rows: ContainerFormRows;
		submitting?: boolean;
		cancelHref?: string;
		onSubmit: () => void;
	} = $props();

	// The form instance is created once by the page and never swapped.
	// svelte-ignore state_referenced_locally
	const inputs = form.inputs;
	const envId = $derived(environmentStore.selected?.id || '0');

	let selectedTab = $state('general');
	let formElement: HTMLFormElement | undefined;

	const defaults = emptyContainerFormValues();
	const advancedCount = $derived(
		Object.entries($inputs).filter(
			([key, input]) =>
				!['name', 'image', 'restartPolicy', 'restartMaxRetries'].includes(key) &&
				input.value !== defaults[key as keyof typeof defaults]
		).length +
			rows.capAdd.length +
			rows.capDrop.length
	);
	const tabItems: TabItem[] = $derived([
		{ value: 'general', label: m.common_general(), icon: ContainersIcon },
		{
			value: 'environment',
			label: m.variables_title(),
			icon: VariableIcon,
			badge: rows.env.filter((row) => row.key.trim()).length + rows.labels.filter((row) => row.key.trim()).length || undefined
		},
		{
			value: 'ports',
			badge: rows.ports.filter((row) => row.containerPort.trim()).length || undefined,
			label: m.common_ports(),
			icon: NetworksIcon
		},
		{
			value: 'volumes',
			badge: rows.volumes.filter((row) => row.target.trim()).length || undefined,
			label: m.resource_volumes_cap(),
			icon: VolumesIcon
		},
		{
			value: 'networks',
			badge: rows.networks.filter((row) => row.network.trim()).length || undefined,
			label: m.resource_networks_cap(),
			icon: NetworksIcon
		},
		{ value: 'advanced', label: m.common_advanced(), icon: SettingsIcon, badge: advancedCount || undefined }
	]);

	const restartPolicies = [
		{ value: 'no', label: m.common_no() },
		{ value: 'always', label: m.common_always() },
		{ value: 'unless-stopped', label: m.restart_policy_unless_stopped() },
		{ value: 'on-failure', label: m.restart_policy_on_failure() }
	];

	const healthModes = [
		{ value: 'inherit', label: m.health_inherit_from_image() },
		{ value: 'custom', label: m.common_custom() },
		{ value: 'disable', label: m.common_disabled() }
	];

	const listOptions = { pagination: { page: 1, limit: 500 } };
	const volumesQuery = createQuery(() => ({
		queryKey: queryKeys.volumes.table(envId, listOptions),
		enabled: !!environmentStore.selected && hasPermission('volumes:list', envId),
		queryFn: () => volumeService.getVolumesForEnvironment(envId, listOptions)
	}));
	const networksQuery = createQuery(() => ({
		queryKey: queryKeys.networks.list(envId, listOptions),
		enabled: !!environmentStore.selected && hasPermission('networks:list', envId),
		queryFn: () => networkService.getNetworksForEnvironment(envId, listOptions)
	}));

	const volumeNames = $derived((volumesQuery.data?.data ?? []).map((volume) => volume.name));
	const networkNames = $derived(
		(networksQuery.data?.data ?? []).map((network) => network.name).filter((name) => name !== 'host' && name !== 'none')
	);

	// Image suggestions (debounced registry search); free text stays valid.
	let imageSuggestions = $state<ImageSearchResultDto[]>([]);
	let imageSearchTimer: ReturnType<typeof setTimeout> | undefined;
	function onImageInput() {
		clearTimeout(imageSearchTimer);
		const term = $inputs.image.value.trim();
		if (!hasPermission('images:read', envId) || term.length < 2 || term.includes(':')) {
			imageSuggestions = [];
			return;
		}
		const searchEnvId = envId;
		imageSearchTimer = setTimeout(async () => {
			if (searchEnvId !== envId || !hasPermission('images:read', envId)) return;
			try {
				const results = await imageService.searchImages(term);
				if (searchEnvId === envId && term === $inputs.image.value.trim()) imageSuggestions = results.slice(0, 6);
			} catch {
				imageSuggestions = [];
			}
		}, 350);
	}

	onDestroy(() => clearTimeout(imageSearchTimer));

	function applyImageSuggestion(name: string) {
		form.setValue('image', name);
		imageSuggestions = [];
	}

	async function handleSubmit() {
		onSubmit();
		const firstError = Object.entries($inputs).find(([, input]) => input.error);
		if (!firstError) return;
		selectedTab = ['name', 'image', 'restartPolicy', 'restartMaxRetries'].includes(firstError[0]) ? 'general' : 'advanced';
		await tick();
		formElement?.querySelector<HTMLInputElement>('[data-invalid="true"] input, input[aria-invalid="true"]')?.focus();
	}

	const availableCapAdd = $derived(
		LINUX_CAPABILITIES.map((cap) => ({ value: cap, label: cap })).filter((item) => !rows.capAdd.includes(item.value))
	);
	const availableCapDrop = $derived(
		[{ value: 'ALL', label: 'ALL' }, ...LINUX_CAPABILITIES.map((cap) => ({ value: cap, label: cap }))].filter(
			(item) => !rows.capDrop.includes(item.value)
		)
	);
</script>

{#snippet capabilityBadge(cap: string, onRemove: () => void)}
	<Badge variant="secondary" class="gap-1 font-mono text-xs">
		{cap}
		<button type="button" onclick={onRemove} disabled={submitting} aria-label={`${m.common_remove()} ${cap}`}>
			<CloseIcon class="size-3" />
		</button>
	</Badge>
{/snippet}

{#snippet groupTitle(title: string, description?: string)}
	<div>
		<h3 class="text-base font-semibold">{title}</h3>
		{#if description}
			<p class="mt-1 text-xs text-muted-foreground">{description}</p>
		{/if}
	</div>
{/snippet}

<form bind:this={formElement} class="flex min-h-0 flex-col" onsubmit={preventDefault(handleSubmit)}>
	{#if environmentStore.selected}
		<p class="mb-3 text-xs break-words text-muted-foreground">{m.resource_environment_cap()}: {environmentStore.selected.name}</p>
	{/if}
	<Tabs.Root value={selectedTab} class="flex min-h-0 flex-1 flex-col">
		<div class="border-b pb-3">
			<TabBar items={tabItems} value={selectedTab} onValueChange={(value) => (selectedTab = value)} />
		</div>

		<div class="flex-1 py-6">
			<!-- General -->
			<Tabs.Content value="general" class="mt-0 space-y-6">
				<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
					<div class="space-y-2">
						<Label for="container-name" class="text-sm font-medium">
							{m.container_name_label()} <span class="text-destructive">*</span>
						</Label>
						<Input
							id="container-name"
							aria-invalid={!!$inputs.name.error}
							aria-describedby={$inputs.name.error ? 'container-name-error' : undefined}
							type="text"
							placeholder={m.container_name_placeholder()}
							disabled={submitting}
							bind:value={$inputs.name.value}
							class={$inputs.name.error ? 'border-destructive' : ''}
						/>
						{#if $inputs.name.error}
							<p id="container-name-error" class="text-xs text-destructive">{$inputs.name.error}</p>
						{/if}
					</div>
					<div class="relative space-y-2">
						<Label for="container-image" class="text-sm font-medium">
							{m.common_image()} <span class="text-destructive">*</span>
						</Label>
						<Input
							id="container-image"
							aria-invalid={!!$inputs.image.error}
							aria-describedby={$inputs.image.error ? 'container-image-error' : undefined}
							type="text"
							placeholder={m.nginx_latest_placeholder()}
							disabled={submitting}
							bind:value={$inputs.image.value}
							oninput={onImageInput}
							class={$inputs.image.error ? 'border-destructive' : ''}
							autocomplete="off"
						/>
						{#if imageSuggestions.length > 0}
							<div class="absolute z-10 w-full rounded-md border bg-popover shadow-md">
								{#each imageSuggestions as suggestion (suggestion.name)}
									<button
										type="button"
										class="block w-full px-3 py-2 text-left text-sm hover:bg-accent"
										onclick={() => applyImageSuggestion(suggestion.name)}
									>
										<span class="font-mono">{suggestion.name}</span>
										{#if suggestion.official}
											<Badge variant="secondary" class="ml-2 text-[10px]">{m.official()}</Badge>
										{/if}
									</button>
								{/each}
							</div>
						{/if}
						{#if $inputs.image.error}
							<p id="container-image-error" class="text-xs text-destructive">{$inputs.image.error}</p>
						{/if}
						{#if mode === 'edit'}
							<p class="text-xs text-muted-foreground">{m.image_pull_if_missing_note()}</p>
						{/if}
					</div>
				</div>
				<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
					<SelectWithLabel
						id="restart-policy"
						bind:value={$inputs.restartPolicy.value}
						label={m.common_restart_policy()}
						options={restartPolicies}
						placeholder={m.container_select_restart_policy()}
						disabled={submitting}
					/>
					{#if $inputs.restartPolicy.value === 'on-failure' || $inputs.restartMaxRetries.error}
						<FormInput
							label={m.max_retry_label()}
							type="number"
							placeholder={m.max_retry_placeholder()}
							disabled={submitting}
							data-invalid={!!$inputs.restartMaxRetries.error}
							bind:input={$inputs.restartMaxRetries}
						/>
					{/if}
				</div>
			</Tabs.Content>

			<!-- Environment (env vars + labels) -->
			<Tabs.Content value="environment" class="mt-0 space-y-8">
				<div class="space-y-4">
					{@render groupTitle(m.common_environment_variables())}
					<KeyValueEditor bind:rows={rows.env} disabled={submitting} />
				</div>
				<div class="space-y-4">
					{@render groupTitle(m.common_labels())}
					<KeyValueEditor
						bind:rows={rows.labels}
						disabled={submitting}
						keyPlaceholder="com.example.key"
						valuePlaceholder="value"
					/>
				</div>
			</Tabs.Content>

			<!-- Ports -->
			<Tabs.Content value="ports" class="mt-0 space-y-4">
				{@render groupTitle(m.common_port_mappings())}
				<PortMappingEditor bind:rows={rows.ports} disabled={submitting} />
			</Tabs.Content>

			<!-- Volumes -->
			<Tabs.Content value="volumes" class="mt-0 space-y-4">
				{@render groupTitle(m.resource_volumes_cap(), mode === 'edit' ? m.mount_options_note() : undefined)}
				<VolumeMountEditor bind:rows={rows.volumes} volumes={volumeNames} disabled={submitting} />
			</Tabs.Content>

			<!-- Networks -->
			<Tabs.Content value="networks" class="mt-0 space-y-4">
				{@render groupTitle(m.resource_networks_cap(), m.aliases_note())}
				<NetworkAttachmentEditor bind:rows={rows.networks} networks={networkNames} disabled={submitting} />
			</Tabs.Content>

			<!-- Advanced (resources, security, healthcheck) -->
			<Tabs.Content value="advanced" class="mt-0 space-y-8">
				<div class="space-y-4">
					{@render groupTitle(m.runtime())}
					<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
						<FormInput
							label={m.common_command()}
							type="text"
							placeholder={m.container_command_placeholder()}
							disabled={submitting}
							data-invalid={!!$inputs.command.error}
							bind:input={$inputs.command}
						/>
						<FormInput
							label={m.common_entrypoint()}
							type="text"
							placeholder="/docker-entrypoint.sh"
							disabled={submitting}
							data-invalid={!!$inputs.entrypoint.error}
							bind:input={$inputs.entrypoint}
						/>
						<FormInput
							label={m.common_working_directory()}
							type="text"
							placeholder={m.app_placeholder()}
							disabled={submitting}
							data-invalid={!!$inputs.workingDir.error}
							bind:input={$inputs.workingDir}
						/>
						<FormInput
							label={m.common_user()}
							type="text"
							placeholder={m.container_user_placeholder()}
							disabled={submitting}
							data-invalid={!!$inputs.user.error}
							bind:input={$inputs.user}
						/>
					</div>
				</div>
				<div class="space-y-4">
					{@render groupTitle(m.common_resources())}
					<div class="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
						<FormInput
							label={m.memory_limit_mb()}
							type="number"
							placeholder="0"
							disabled={submitting}
							data-invalid={!!$inputs.memoryMb.error}
							bind:input={$inputs.memoryMb}
						/>
						<FormInput
							label={m.memory_swap_mb()}
							type="number"
							placeholder="0"
							disabled={submitting}
							data-invalid={!!$inputs.memorySwapMb.error}
							bind:input={$inputs.memorySwapMb}
						/>
						<FormInput
							label={m.common_cpus()}
							type="number"
							placeholder="0"
							disabled={submitting}
							data-invalid={!!$inputs.cpus.error}
							bind:input={$inputs.cpus}
						/>
						<FormInput
							label={m.cpu_shares()}
							type="number"
							placeholder="0"
							disabled={submitting}
							data-invalid={!!$inputs.cpuShares.error}
							bind:input={$inputs.cpuShares}
						/>
					</div>

					<div class="flex items-center space-x-2">
						<Checkbox id="auto-remove" bind:checked={$inputs.autoRemove.value} disabled={submitting} />
						<Label for="auto-remove" class="text-sm font-normal">{m.auto_remove_label()}</Label>
					</div>
				</div>

				<div class="space-y-4 border-t border-border/50 pt-6">
					{@render groupTitle(m.common_security())}
					<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
						<div class="flex items-center space-x-2">
							<Checkbox id="privileged" bind:checked={$inputs.privileged.value} disabled={submitting} />
							<Label for="privileged" class="text-sm font-normal">{m.privileged_label()}</Label>
						</div>
						<div class="flex items-center space-x-2">
							<Checkbox id="readonly-rootfs" bind:checked={$inputs.readonlyRootfs.value} disabled={submitting} />
							<Label for="readonly-rootfs" class="text-sm font-normal">{m.readonly_rootfs_label()}</Label>
						</div>
					</div>
					<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
						<div class="space-y-2">
							<Label class="text-sm font-medium">{m.cap_add()}</Label>
							<div class="flex flex-wrap gap-1.5">
								{#each rows.capAdd as cap (cap)}
									{@render capabilityBadge(cap, () => (rows.capAdd = rows.capAdd.filter((value) => value !== cap)))}
								{/each}
							</div>
							<SearchableSelect
								items={availableCapAdd}
								showCheckboxes={false}
								disabled={submitting}
								onSelect={(cap) => {
									if (cap) rows.capAdd = [...rows.capAdd, cap];
								}}
							/>
						</div>
						<div class="space-y-2">
							<Label class="text-sm font-medium">{m.cap_drop()}</Label>
							<div class="flex flex-wrap gap-1.5">
								{#each rows.capDrop as cap (cap)}
									{@render capabilityBadge(cap, () => (rows.capDrop = rows.capDrop.filter((value) => value !== cap)))}
								{/each}
							</div>
							<SearchableSelect
								items={availableCapDrop}
								showCheckboxes={false}
								disabled={submitting}
								onSelect={(cap) => {
									if (cap) rows.capDrop = [...rows.capDrop, cap];
								}}
							/>
						</div>
					</div>
				</div>

				<div class="space-y-4 border-t border-border/50 pt-6">
					{@render groupTitle(m.health_configuration())}
					<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
						<SelectWithLabel
							id="health-mode"
							bind:value={$inputs.healthMode.value}
							label={m.common_mode()}
							options={healthModes}
							disabled={submitting}
						/>
						{#if $inputs.healthMode.value === 'custom' || $inputs.healthTest.error}
							<FormInput
								label={m.health_test_command()}
								type="text"
								placeholder="curl -f http://localhost/ || exit 1"
								disabled={submitting}
								data-invalid={!!$inputs.healthTest.error}
								bind:input={$inputs.healthTest}
							/>
						{/if}
					</div>
					{#if $inputs.healthMode.value === 'custom' || $inputs.healthInterval.error || $inputs.healthTimeout.error || $inputs.healthStartPeriod.error || $inputs.healthRetries.error}
						<div class="grid grid-cols-2 gap-6 lg:grid-cols-4">
							<FormInput
								label={`${m.health_interval()} (s)`}
								type="number"
								placeholder="30"
								disabled={submitting}
								data-invalid={!!$inputs.healthInterval.error}
								bind:input={$inputs.healthInterval}
							/>
							<FormInput
								label={`${m.health_timeout()} (s)`}
								type="number"
								placeholder="30"
								disabled={submitting}
								data-invalid={!!$inputs.healthTimeout.error}
								bind:input={$inputs.healthTimeout}
							/>
							<FormInput
								label={`${m.health_start_period()} (s)`}
								type="number"
								placeholder="0"
								disabled={submitting}
								data-invalid={!!$inputs.healthStartPeriod.error}
								bind:input={$inputs.healthStartPeriod}
							/>
							<FormInput
								label={m.health_retries()}
								type="number"
								placeholder="3"
								disabled={submitting}
								data-invalid={!!$inputs.healthRetries.error}
								bind:input={$inputs.healthRetries}
							/>
						</div>
					{/if}
				</div>
			</Tabs.Content>
		</div>
	</Tabs.Root>

	<div class="flex flex-col-reverse gap-2 border-t pt-4 pb-6 sm:flex-row sm:justify-end">
		<ArcaneButton action="cancel" tone="outline" href={cancelHref} disabled={submitting} class="w-full sm:w-auto" />
		<ArcaneButton
			action={mode === 'create' ? 'create' : 'save'}
			type="submit"
			disabled={submitting}
			loading={submitting}
			class="w-full sm:w-auto"
			customLabel={mode === 'create' ? m.common_create_button({ resource: m.container() }) : m.common_save()}
		/>
	</div>
</form>

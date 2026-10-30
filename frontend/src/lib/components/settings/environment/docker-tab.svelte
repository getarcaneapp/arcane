<script lang="ts">
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import PruneModePicker from '#lib/components/prune/prune-mode-picker.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { UpdateCheckRegistry } from '#lib/types/settings.js';
	import {
		arcaneImageRegistryOptions,
		arcaneToolsImage,
		arcaneUpdateCheckImage,
		arcaneUpdateCheckRegistryOptions
	} from '#lib/utils/registry.js';

	import type { DockerTabProps } from './tab-props';

	let { formInputs = $bindable(), environmentId, mixed }: DockerTabProps = $props();

	const shellOptions = [
		{
			value: '/bin/sh',
			label: '/bin/sh',
			description: m.docker_shell_sh_description()
		},
		{
			value: '/bin/bash',
			label: '/bin/bash',
			description: m.docker_shell_bash_description()
		},
		{
			value: '/bin/ash',
			label: '/bin/ash',
			description: m.docker_shell_ash_description()
		},
		{
			value: '/bin/zsh',
			label: '/bin/zsh',
			description: m.docker_shell_zsh_description()
		}
	];

	let customShell = $state(false);
	const shellSelectValue = $derived.by(() => {
		if (customShell) return 'custom';
		if (mixed('defaultShell')) return '';
		if (!shellOptions.some((option) => option.value === formInputs.defaultShell.value)) return 'custom';
		return formInputs.defaultShell.value;
	});

	function handleShellSelectChange(value: string) {
		customShell = value === 'custom';
		if (!customShell) formInputs.defaultShell.value = value;
	}

	const deployPullPolicyOptions = [
		{ value: 'missing', label: 'Missing', description: m.deploy_pull_policy_missing() },
		{ value: 'always', label: m.common_always(), description: m.deploy_pull_policy_always() },
		{ value: 'never', label: m.common_never(), description: m.deploy_pull_policy_never() }
	];

	const registryOptions = arcaneImageRegistryOptions();
	const updateCheckRegistryOptions = arcaneUpdateCheckRegistryOptions();
	const updateCheckImage = $derived(arcaneUpdateCheckImage(formInputs.updateCheckRegistry.value, environmentId === '0'));

	const pruneContainerModes = [
		{ value: 'none', label: m.none() },
		{ value: 'stopped', label: m.prune_stopped_containers() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneImageModes = [
		{ value: 'none', label: m.none() },
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.all_unused() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneVolumeModes = [
		{ value: 'none', label: m.none() },
		{ value: 'anonymous', label: m.prune_volumes_mode_anonymous() },
		{ value: 'all', label: m.all_unused(), destructive: true }
	];
	const pruneNetworkModes = [
		{ value: 'none', label: m.none() },
		{ value: 'unused', label: m.unused_networks() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
	const pruneBuildCacheModes = [
		{ value: 'none', label: m.none() },
		{ value: 'unused', label: m.prune_build_cache_mode_unused() },
		{ value: 'all', label: m.prune_build_cache_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];
</script>

<div class="space-y-8">
	<SettingsSection title={m.environments_docker_settings_title()} description={m.environments_config_description()}>
		<SettingsRow
			for="shellSelectValue"
			label={m.docker_default_shell_label()}
			description={m.docker_default_shell_description()}
			error={formInputs.defaultShell.error}
			helpText={shellSelectValue === 'custom' ? m.docker_shell_custom_path_help() : undefined}
			mixed={mixed('defaultShell')}
		>
			<SelectWithLabel
				id="shellSelectValue"
				name="shellSelectValue"
				hideLabel
				value={shellSelectValue}
				onValueChange={handleShellSelectChange}
				label={m.docker_default_shell_label()}
				placeholder={mixed('defaultShell') ? m.mixed() : m.select_shell_placeholder()}
				options={[...shellOptions, { value: 'custom', label: m.custom(), description: m.docker_shell_custom_description() }]}
			/>
			{#if shellSelectValue === 'custom'}
				<Input
					class="mt-2"
					bind:value={formInputs.defaultShell.value}
					placeholder={mixed('defaultShell') ? m.mixed() : m.bin_sh_placeholder()}
					aria-label={m.custom()}
					aria-invalid={!!formInputs.defaultShell.error}
				/>
			{/if}
		</SettingsRow>

		<SettingsRow
			for="defaultDeployPullPolicy"
			label={m.settings_default_deploy_pull_policy()}
			description={m.settings_default_deploy_pull_policy_description()}
			mixed={mixed('defaultDeployPullPolicy')}
		>
			<SelectWithLabel
				id="defaultDeployPullPolicy"
				name="defaultDeployPullPolicy"
				hideLabel
				bind:value={formInputs.defaultDeployPullPolicy.value}
				label={m.settings_default_deploy_pull_policy()}
				options={deployPullPolicyOptions}
				onValueChange={(v) => (formInputs.defaultDeployPullPolicy.value = v as 'missing' | 'always' | 'never')}
				placeholder={mixed('defaultDeployPullPolicy') ? m.mixed() : undefined}
			/>
		</SettingsRow>

		<SettingsRow
			for="toolsImageRegistry"
			label={m.tools_image_registry_label()}
			description={m.tools_image_registry_description()}
			mixed={mixed('toolsImageRegistry')}
		>
			<SelectWithLabel
				id="toolsImageRegistry"
				name="toolsImageRegistry"
				hideLabel
				bind:value={formInputs.toolsImageRegistry.value}
				label={m.tools_image_registry_label()}
				options={registryOptions}
				onValueChange={(v) => (formInputs.toolsImageRegistry.value = v as 'ghcr.io' | 'docker.io')}
				placeholder={mixed('toolsImageRegistry') ? m.mixed() : undefined}
			/>
			{#snippet helpText()}
				<span class="font-mono">{arcaneToolsImage(formInputs.toolsImageRegistry.value)}</span>
			{/snippet}
		</SettingsRow>

		<SettingsRow
			for="updateCheckRegistry"
			label={m.update_check_registry_label()}
			description={m.update_check_registry_description()}
			mixed={mixed('updateCheckRegistry')}
		>
			<SelectWithLabel
				id="updateCheckRegistry"
				name="updateCheckRegistry"
				hideLabel
				bind:value={formInputs.updateCheckRegistry.value}
				label={m.update_check_registry_label()}
				options={updateCheckRegistryOptions}
				onValueChange={(v) => (formInputs.updateCheckRegistry.value = v as UpdateCheckRegistry)}
				placeholder={mixed('updateCheckRegistry') ? m.mixed() : undefined}
			/>
			{#snippet helpText()}
				{#if updateCheckImage}<span class="font-mono">{updateCheckImage}</span>{:else}{m.follows_running_image()}{/if}
			{/snippet}
		</SettingsRow>

		<SettingsRow
			for="base-server-url"
			label={m.general_base_url_label()}
			description={m.general_base_url_help()}
			error={formInputs.baseServerUrl.error}
			mixed={mixed('baseServerUrl')}
		>
			<Input
				id="base-server-url"
				bind:value={formInputs.baseServerUrl.value}
				aria-invalid={!!formInputs.baseServerUrl.error}
				placeholder={mixed('baseServerUrl') ? m.mixed() : undefined}
			/>
		</SettingsRow>

		<SettingsRow
			for="auto-inject-env"
			label={m.docker_auto_inject_env_label()}
			description={m.docker_auto_inject_env_description()}
			layout="switch"
			mixed={mixed('autoInjectEnv')}
		>
			<Switch
				id="auto-inject-env"
				bind:checked={formInputs.autoInjectEnv.value}
				data-mixed={mixed('autoInjectEnv') || undefined}
			/>
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.prune_options_title()} description={m.prune_options_description()}>
		<SettingsRow
			label={m.containers()}
			description={m.scheduled_prune_containers_description()}
			layout="wide"
			mixed={mixed('pruneContainerMode') || mixed('pruneContainerUntil')}
		>
			<PruneModePicker
				modeOptions={pruneContainerModes}
				bind:value={formInputs.pruneContainerMode.value}
				bind:untilValue={formInputs.pruneContainerUntil.value}
			/>
		</SettingsRow>
		<SettingsRow
			label={m.images()}
			description={m.scheduled_prune_images_description()}
			layout="wide"
			mixed={mixed('pruneImageMode') || mixed('pruneImageUntil')}
		>
			<PruneModePicker
				modeOptions={pruneImageModes}
				bind:value={formInputs.pruneImageMode.value}
				bind:untilValue={formInputs.pruneImageUntil.value}
			/>
		</SettingsRow>
		<SettingsRow
			label={m.resource_volumes_cap()}
			description={m.scheduled_prune_volumes_description()}
			layout="wide"
			mixed={mixed('pruneVolumeMode')}
		>
			<PruneModePicker
				modeOptions={pruneVolumeModes}
				bind:value={formInputs.pruneVolumeMode.value}
				warningTitle={m.prune_volumes_warning_title()}
				warningDescription={m.scheduled_prune_volumes_warning()}
			/>
		</SettingsRow>
		<SettingsRow
			label={m.resource_networks_cap()}
			description={m.scheduled_prune_networks_description()}
			layout="wide"
			mixed={mixed('pruneNetworkMode') || mixed('pruneNetworkUntil')}
		>
			<PruneModePicker
				modeOptions={pruneNetworkModes}
				bind:value={formInputs.pruneNetworkMode.value}
				bind:untilValue={formInputs.pruneNetworkUntil.value}
			/>
		</SettingsRow>
		<SettingsRow
			label={m.build_cache()}
			description={m.scheduled_prune_build_cache_description()}
			layout="wide"
			mixed={mixed('pruneBuildCacheMode') || mixed('pruneBuildCacheUntil')}
		>
			<PruneModePicker
				modeOptions={pruneBuildCacheModes}
				bind:value={formInputs.pruneBuildCacheMode.value}
				bind:untilValue={formInputs.pruneBuildCacheUntil.value}
			/>
		</SettingsRow>
	</SettingsSection>
</div>

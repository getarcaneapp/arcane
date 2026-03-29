<script lang="ts">
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { TestIcon, SettingsIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { Environment } from '$lib/types/environment.type';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Switch } from '$lib/components/ui/switch/index.js';

	let {
		formInputs,
		environment,
		isTestingConnection = false,
		testConnection,
		showSettingsSections = true
	}: {
		formInputs: any;
		environment: Environment;
		isTestingConnection?: boolean;
		testConnection?: () => void;
		showSettingsSections?: boolean;
	} = $props();
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={SettingsIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.general_title()}</h2>
			</Card.Title>
			<Card.Description>{m.environments_config_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="space-y-6 p-4">
		<div class="space-y-6 rounded-lg border p-4">
			<div class="space-y-0.5">
				<h3 class="text-sm font-medium">{m.resource_environment_cap()}</h3>
				<div class="text-muted-foreground text-xs">{m.environments_basic_info_description()}</div>
			</div>

			<div class="grid gap-6 sm:grid-cols-2">
				<div class="space-y-2">
					<TextInputWithLabel
						id="env-name"
						label={m.common_name()}
						bind:value={$formInputs.name.value}
						error={$formInputs.name.error}
						helpText={m.environments_name_placeholder()}
					/>
				</div>

				<div class="space-y-4 rounded-lg border p-4">
					<div class="flex items-center justify-between gap-4">
						<div class="space-y-0.5">
							<Label for="env-enabled" class="text-sm font-medium">{m.common_enabled()}</Label>
							<div class="text-muted-foreground text-xs">{m.environments_enable_disable_description()}</div>
						</div>
						{#if environment.id === '0'}
							<ArcaneTooltip.Root>
								<ArcaneTooltip.Trigger>
									<Switch id="env-enabled" disabled={true} bind:checked={$formInputs.enabled.value} />
								</ArcaneTooltip.Trigger>
								<ArcaneTooltip.Content>
									<p>{m.environments_local_setting_disabled()}</p>
								</ArcaneTooltip.Content>
							</ArcaneTooltip.Root>
						{:else}
							<Switch id="env-enabled" bind:checked={$formInputs.enabled.value} />
						{/if}
					</div>
				</div>

				<div class="space-y-2 sm:col-span-2">
					<Label for="api-url" class="text-sm font-medium">{m.environments_api_url()}</Label>
					<div class="mt-1.5 flex items-center gap-2">
						{#if environment.id === '0'}
							<ArcaneTooltip.Root>
								<ArcaneTooltip.Trigger class="w-full">
									<Input
										id="api-url"
										type="url"
										bind:value={$formInputs.apiUrl.value}
										class="w-full font-mono"
										placeholder={m.environments_api_url_placeholder()}
										disabled={true}
										required
									/>
								</ArcaneTooltip.Trigger>
								<ArcaneTooltip.Content>
									<p>{m.environments_local_setting_disabled()}</p>
								</ArcaneTooltip.Content>
							</ArcaneTooltip.Root>
						{:else}
							<Input
								id="api-url"
								type="url"
								bind:value={$formInputs.apiUrl.value}
								class="w-full font-mono"
								placeholder={m.environments_api_url_placeholder()}
								required
							/>
						{/if}
						{#if testConnection}
							<ArcaneButton
								action="base"
								onclick={testConnection}
								disabled={isTestingConnection}
								loading={isTestingConnection}
								icon={TestIcon}
								customLabel={m.environments_test_connection()}
								loadingLabel={m.environments_testing_connection()}
								class="shrink-0"
							/>
						{/if}
					</div>
					<p class="text-muted-foreground mt-1.5 text-xs">{m.environments_api_url_help()}</p>
					{#if $formInputs.apiUrl.error}
						<p class="text-destructive text-[0.8rem] font-medium">{$formInputs.apiUrl.error}</p>
					{/if}
				</div>
			</div>
		</div>

		{#if showSettingsSections}
			<div class="grid gap-6 sm:grid-cols-2">
				<div class="space-y-2">
					<TextInputWithLabel
						id="projects-directory"
						label={m.general_projects_directory_label()}
						bind:value={$formInputs.projectsDirectory.value}
						error={$formInputs.projectsDirectory.error}
						helpText={m.general_projects_directory_help()}
					/>
				</div>
				<div class="space-y-2">
					<TextInputWithLabel
						id="disk-usage-path"
						label={m.disk_usage_settings()}
						bind:value={$formInputs.diskUsagePath.value}
						error={$formInputs.diskUsagePath.error}
						helpText={m.disk_usage_settings_description()}
					/>
				</div>
				<div class="space-y-2">
					<TextInputWithLabel
						id="swarm-stack-sources-directory"
						label="Swarm Stack Sources Directory"
						bind:value={$formInputs.swarmStackSourcesDirectory.value}
						error={$formInputs.swarmStackSourcesDirectory.error}
						helpText="Directory where original compose/env sources for Swarm stack deploys are stored. Supports the same container:host bind-mount format as Projects Directory."
					/>
				</div>
				<div class="space-y-2">
					<TextInputWithLabel
						id="base-server-url"
						label={m.general_base_url_label()}
						bind:value={$formInputs.baseServerUrl.value}
						error={$formInputs.baseServerUrl.error}
						helpText={m.general_base_url_help()}
					/>
				</div>
				<div class="space-y-2">
					<TextInputWithLabel
						id="max-upload-size"
						type="number"
						label={m.docker_max_upload_size_label()}
						bind:value={$formInputs.maxImageUploadSize.value}
						error={$formInputs.maxImageUploadSize.error}
						helpText={m.docker_max_upload_size_description()}
					/>
				</div>
				<div class="space-y-4 rounded-lg border p-4 sm:col-span-2">
					<div class="space-y-0.5">
						<h3 class="text-sm font-medium">{m.git_sync_file_limits_title()}</h3>
						<div class="text-muted-foreground text-xs">{m.git_sync_file_limits_description()}</div>
					</div>
					<div class="grid gap-4 sm:grid-cols-3">
						<TextInputWithLabel
							id="git-sync-max-files"
							type="number"
							label={m.git_sync_max_files_label()}
							bind:value={$formInputs.gitSyncMaxFiles.value}
							error={$formInputs.gitSyncMaxFiles.error}
							helpText={m.git_sync_max_files_help()}
						/>
						<TextInputWithLabel
							id="git-sync-max-total-size"
							type="number"
							label={m.git_sync_max_total_size_label()}
							bind:value={$formInputs.gitSyncMaxTotalSizeMb.value}
							error={$formInputs.gitSyncMaxTotalSizeMb.error}
							helpText={m.git_sync_max_total_size_help()}
						/>
						<TextInputWithLabel
							id="git-sync-max-binary-size"
							type="number"
							label={m.git_sync_max_binary_size_label()}
							bind:value={$formInputs.gitSyncMaxBinarySizeMb.value}
							error={$formInputs.gitSyncMaxBinarySizeMb.error}
							helpText={m.git_sync_max_binary_size_help()}
						/>
					</div>
				</div>
				<div class="space-y-4 rounded-lg border p-4 sm:col-span-2">
					<div class="flex items-center justify-between gap-4">
						<div class="space-y-0.5">
							<Label for="follow-project-symlinks" class="text-sm font-medium">
								{m.general_follow_project_symlinks_label()}
							</Label>
							<div class="text-muted-foreground text-xs">{m.general_follow_project_symlinks_help()}</div>
						</div>
						<Switch id="follow-project-symlinks" bind:checked={$formInputs.followProjectSymlinks.value} />
					</div>
				</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>

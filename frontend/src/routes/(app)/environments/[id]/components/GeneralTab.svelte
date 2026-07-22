<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '$lib/components/settings/settings-row.svelte';
	import EnvironmentConnectionDetails from './EnvironmentConnectionDetails.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SettingsIcon, TestIcon } from '$lib/icons';
	import type { GeneralTabProps } from './tab-props';

	let { formInputs, environment, currentStatus, isTestingConnection, testConnection, settingsAvailable }: GeneralTabProps =
		$props();
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
		<!-- Identity / editable fields -->
		<div class="space-y-4">
			<div>
				<Label for="env-name" class="text-sm font-medium">{m.common_name()}</Label>
				<Input
					id="env-name"
					type="text"
					bind:value={$formInputs.name.value}
					class="mt-1.5 w-full {$formInputs.name.error ? 'border-destructive' : ''}"
					placeholder={m.environments_name_placeholder()}
				/>
				{#if $formInputs.name.error}
					<p class="mt-1 text-[0.8rem] font-medium text-destructive">{$formInputs.name.error}</p>
				{/if}
			</div>

			<div>
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
					<ArcaneButton
						action="base"
						onclick={testConnection}
						disabled={isTestingConnection}
						loading={isTestingConnection}
						icon={TestIcon}
						customLabel={m.test_connection()}
						loadingLabel={m.environments_testing_connection()}
						class="shrink-0"
					/>
				</div>
				<p class="mt-1.5 text-xs text-muted-foreground">{m.environments_api_url_help()}</p>
			</div>
		</div>

		<!-- Edge connection status (read-only) -->
		{#if environment.isEdge}
			<div class="border-t pt-6">
				<EnvironmentConnectionDetails {environment} {currentStatus} />
			</div>
		{/if}

		<!-- Advanced settings -->
		{#if settingsAvailable}
			<div class="space-y-6 border-t pt-6">
				<div class="grid gap-6 sm:grid-cols-2">
					<TextInputWithLabel
						id="projects-directory"
						label={m.general_projects_directory_label()}
						bind:value={$formInputs.projectsDirectory.value}
						error={$formInputs.projectsDirectory.error}
						helpText={m.general_projects_directory_help()}
					/>
					<TextInputWithLabel
						id="templates-directory"
						label={m.general_templates_directory_label()}
						bind:value={$formInputs.templatesDirectory.value}
						error={$formInputs.templatesDirectory.error}
						helpText={m.general_templates_directory_help()}
					/>
					<TextInputWithLabel
						id="disk-usage-path"
						label={m.disk_usage_settings()}
						bind:value={$formInputs.diskUsagePath.value}
						error={$formInputs.diskUsagePath.error}
						helpText={m.disk_usage_settings_description()}
					/>
					<TextInputWithLabel
						id="swarm-stack-sources-directory"
						label={m.environments_swarm_stack_source_label()}
						bind:value={$formInputs.swarmStackSourcesDirectory.value}
						error={$formInputs.swarmStackSourcesDirectory.error}
						helpText="Directory where original compose/env sources for Swarm stack deploys are stored. Supports the same container:host bind-mount format as Projects Directory."
					/>
					<TextInputWithLabel
						id="base-server-url"
						label={m.general_base_url_label()}
						bind:value={$formInputs.baseServerUrl.value}
						error={$formInputs.baseServerUrl.error}
						helpText={m.general_base_url_help()}
					/>
					<TextInputWithLabel
						id="max-upload-size"
						type="number"
						label={m.docker_max_upload_size_label()}
						bind:value={$formInputs.maxImageUploadSize.value}
						error={$formInputs.maxImageUploadSize.error}
						helpText={m.docker_max_upload_size_description()}
					/>
				</div>

				<div class="space-y-4 border-t pt-6">
					<div class="space-y-0.5">
						<h3 class="text-sm font-medium">{m.git_sync_file_limits_title()}</h3>
						<div class="text-xs text-muted-foreground">{m.git_sync_file_limits_description()}</div>
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

				<div class="border-t pt-6">
					<SettingsRow
						layout="inline"
						label={m.general_follow_project_symlinks_label()}
						description={m.general_follow_project_symlinks_help()}
					>
						<Switch id="follow-project-symlinks" bind:checked={$formInputs.followProjectSymlinks.value} />
					</SettingsRow>
				</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>

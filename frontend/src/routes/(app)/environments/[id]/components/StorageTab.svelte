<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import { m } from '#lib/paraglide/messages';
	import { FolderOpenIcon, UploadIcon } from '#lib/icons';
	import type { StorageTabProps } from './tab-props';

	let { formInputs }: StorageTabProps = $props();
</script>

<div class="space-y-6">
	<Card.Root class="flex flex-col">
		<Card.Header icon={FolderOpenIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.directories_storage_paths()}</h2>
				</Card.Title>
				<Card.Description>{m.directories_storage_paths_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
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
					id="swarm-stack-sources-directory"
					label={m.environments_swarm_stack_source_label()}
					bind:value={$formInputs.swarmStackSourcesDirectory.value}
					error={$formInputs.swarmStackSourcesDirectory.error}
					helpText={m.environments_swarm_stack_source_help()}
				/>
				<TextInputWithLabel
					id="disk-usage-path"
					label={m.disk_usage_settings()}
					bind:value={$formInputs.diskUsagePath.value}
					error={$formInputs.diskUsagePath.error}
					helpText={m.disk_usage_settings_description()}
				/>
			</div>

			<div class="mt-6 border-t pt-6">
				<SettingsRow
					layout="inline"
					label={m.general_follow_project_symlinks_label()}
					description={m.general_follow_project_symlinks_help()}
				>
					<Switch id="follow-project-symlinks" bind:checked={$formInputs.followProjectSymlinks.value} />
				</SettingsRow>
			</div>
		</Card.Content>
	</Card.Root>

	<Card.Root class="flex flex-col">
		<Card.Header icon={UploadIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.sync_upload_limits()}</h2>
				</Card.Title>
				<Card.Description>{m.sync_upload_limits_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="space-y-6 p-4">
			<div class="grid gap-6 sm:grid-cols-2">
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
		</Card.Content>
	</Card.Root>
</div>

<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { StorageTabProps } from './tab-props';

	let { formInputs = $bindable(), settings, mixed }: StorageTabProps = $props();

	const supportsUnmanagedBackupRetention = $derived(settings.values.unmanagedBackupRetentionDays !== undefined);
</script>

<div class="space-y-8">
	<SettingsSection title={m.directories_storage_paths()} description={m.directories_storage_paths_description()}>
		<SettingsRow
			for="projects-directory"
			label={m.general_projects_directory_label()}
			description={m.general_projects_directory_help()}
			error={formInputs.projectsDirectory.error}
			mixed={mixed('projectsDirectory')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="projects-directory"
					bind:value={formInputs.projectsDirectory.value}
					aria-invalid={!!formInputs.projectsDirectory.error}
					placeholder={mixed('projectsDirectory') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="templates-directory"
			label={m.general_templates_directory_label()}
			description={m.general_templates_directory_help()}
			error={formInputs.templatesDirectory.error}
			mixed={mixed('templatesDirectory')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="templates-directory"
					bind:value={formInputs.templatesDirectory.value}
					aria-invalid={!!formInputs.templatesDirectory.error}
					placeholder={mixed('templatesDirectory') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="swarm-stack-sources-directory"
			label={m.environments_swarm_stack_source_label()}
			description={m.environments_swarm_stack_source_help()}
			error={formInputs.swarmStackSourcesDirectory.error}
			mixed={mixed('swarmStackSourcesDirectory')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="swarm-stack-sources-directory"
					bind:value={formInputs.swarmStackSourcesDirectory.value}
					aria-invalid={!!formInputs.swarmStackSourcesDirectory.error}
					placeholder={mixed('swarmStackSourcesDirectory') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="disk-usage-path"
			label={m.disk_usage_settings()}
			description={m.disk_usage_settings_description()}
			error={formInputs.diskUsagePath.error}
			mixed={mixed('diskUsagePath')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="disk-usage-path"
					bind:value={formInputs.diskUsagePath.value}
					aria-invalid={!!formInputs.diskUsagePath.error}
					placeholder={mixed('diskUsagePath') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="follow-project-symlinks"
			label={m.general_follow_project_symlinks_label()}
			description={m.general_follow_project_symlinks_help()}
			layout="switch"
			mixed={mixed('followProjectSymlinks')}
		>
			{#snippet children({ describedBy })}
				<Switch
					id="follow-project-symlinks"
					bind:checked={formInputs.followProjectSymlinks.value}
					data-mixed={!!mixed('followProjectSymlinks') || undefined}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.sync_upload_limits()} description={m.sync_upload_limits_description()}>
		<SettingsRow
			for="max-upload-size"
			label={m.docker_max_upload_size_label()}
			description={m.docker_max_upload_size_description()}
			error={formInputs.maxImageUploadSize.error}
			mixed={mixed('maxImageUploadSize')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="max-upload-size"
					type="number"
					bind:value={formInputs.maxImageUploadSize.value}
					aria-invalid={!!formInputs.maxImageUploadSize.error}
					placeholder={mixed('maxImageUploadSize') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	<SettingsSection title={m.git_sync_file_limits_title()} description={m.git_sync_file_limits_description()}>
		<SettingsRow
			for="git-sync-max-files"
			label={m.git_sync_max_files_label()}
			description={m.git_sync_max_files_help()}
			error={formInputs.gitSyncMaxFiles.error}
			mixed={mixed('gitSyncMaxFiles')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="git-sync-max-files"
					type="number"
					bind:value={formInputs.gitSyncMaxFiles.value}
					aria-invalid={!!formInputs.gitSyncMaxFiles.error}
					placeholder={mixed('gitSyncMaxFiles') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="git-sync-max-total-size"
			label={m.git_sync_max_total_size_label()}
			description={m.git_sync_max_total_size_help()}
			error={formInputs.gitSyncMaxTotalSizeMb.error}
			mixed={mixed('gitSyncMaxTotalSizeMb')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="git-sync-max-total-size"
					type="number"
					bind:value={formInputs.gitSyncMaxTotalSizeMb.value}
					aria-invalid={!!formInputs.gitSyncMaxTotalSizeMb.error}
					placeholder={mixed('gitSyncMaxTotalSizeMb') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
		<SettingsRow
			for="git-sync-max-binary-size"
			label={m.git_sync_max_binary_size_label()}
			description={m.git_sync_max_binary_size_help()}
			error={formInputs.gitSyncMaxBinarySizeMb.error}
			mixed={mixed('gitSyncMaxBinarySizeMb')}
		>
			{#snippet children({ describedBy, errorId })}
				<Input
					id="git-sync-max-binary-size"
					type="number"
					bind:value={formInputs.gitSyncMaxBinarySizeMb.value}
					aria-invalid={!!formInputs.gitSyncMaxBinarySizeMb.error}
					placeholder={mixed('gitSyncMaxBinarySizeMb') ? m.mixed() : undefined}
					aria-describedby={describedBy}
					aria-errormessage={errorId}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>

	{#if supportsUnmanagedBackupRetention}
		<SettingsSection title={m.backups()} description={m.backup_cleanup_description()}>
			<SettingsRow
				for="unmanaged-backup-retention-days"
				label={m.unmanaged_backup_retention_days()}
				description={m.unmanaged_backup_retention_days_description()}
				helpText={m.unmanaged_backup_retention_days_help()}
				error={formInputs.unmanagedBackupRetentionDays.error}
				mixed={mixed('unmanagedBackupRetentionDays')}
			>
				{#snippet children({ describedBy, errorId })}
					<Input
						id="unmanaged-backup-retention-days"
						type="number"
						bind:value={formInputs.unmanagedBackupRetentionDays.value}
						aria-invalid={!!formInputs.unmanagedBackupRetentionDays.error}
						placeholder={mixed('unmanagedBackupRetentionDays') ? m.mixed() : undefined}
						aria-describedby={describedBy}
						aria-errormessage={errorId}
					/>
				{/snippet}
			</SettingsRow>
		</SettingsSection>
	{/if}
</div>

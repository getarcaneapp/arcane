<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import { SettingsIcon } from '$lib/icons';

	let { formInputs } = $props();
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
		</div>
	</Card.Content>
</Card.Root>

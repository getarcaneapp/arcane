<script lang="ts">
	import type { Snippet } from 'svelte';

	import EnvironmentSettingsScopeBar from '#lib/components/settings/environment-settings-scope-bar.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { useEnvironmentSettingsScope } from '#lib/hooks/use-environment-settings-scope.svelte.js';
	import { ConnectionIcon, type IconType } from '#lib/icons/index.js';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';

	import type { EnvironmentSettingsValues } from './environment-form-schema';
	import EnvironmentSettingsForm from './environment-settings-form.svelte';
	import type { EnvironmentSettingsContent } from './tab-props';

	let {
		title,
		description,
		icon,
		keys,
		permissions = [],
		content
	}: {
		title: string;
		description?: string;
		icon: IconType;
		keys: (keyof EnvironmentSettingsValues)[];
		permissions?: string[];
		content: Snippet<[EnvironmentSettingsContent, ReturnType<typeof useEnvironmentSettingsScope>]>;
	} = $props();

	const scope = useEnvironmentSettingsScope(() => permissions);
	const readOnly = $derived(scope.entries.some(({ settings }) => settings.readOnly));
</script>

<SettingsPageLayout {title} {description} {icon} showReadOnlyTag={readOnly}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<EnvironmentSettingsScopeBar
				selectedId={scope.selectedId}
				environments={scope.environments}
				offlineCount={scope.offlineCount}
			/>
			{#if scope.isLoading}
				<div class="flex h-32 items-center justify-center">
					<Spinner class="size-8" />
				</div>
			{:else if scope.entries.length === 0}
				<EmptyState icon={ConnectionIcon} title={m.environments_settings_offline_title()} />
			{:else}
				<fieldset disabled={readOnly} class="contents">
					{#key scope.version}
						<EnvironmentSettingsForm {scope} {keys} {content} />
					{/key}
				</fieldset>
			{/if}
		</div>
	{/snippet}
</SettingsPageLayout>

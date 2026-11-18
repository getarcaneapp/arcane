<script lang="ts">
	import { Input } from '@arcane/ui/input';
	import { untrack } from 'svelte';
	import { z } from 'zod/v4';

	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { SettingsPageLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { createSettingsForm } from '#lib/utils/settings-form.js';

	const formSchema = z.object({
		allEnvironmentsLimit: z.coerce
			.number()
			.int(m.all_environments_limit_help())
			.min(2, m.all_environments_limit_help())
			.max(256, m.all_environments_limit_help()),
		upgradeMaxParallelAgents: z.coerce
			.number()
			.int(m.upgrade_max_parallel_agents_help())
			.min(1, m.upgrade_max_parallel_agents_help())
			.max(16, m.upgrade_max_parallel_agents_help()),
		frontendCacheMinutes: z.coerce
			.number()
			.int(m.cached_data_lifetime_help())
			.min(1, m.cached_data_lifetime_help())
			.max(120, m.cached_data_lifetime_help())
	});

	function readFormValues() {
		return {
			allEnvironmentsLimit: Number(settingsStore.current?.allEnvironmentsLimit) || 64,
			upgradeMaxParallelAgents: Number(settingsStore.current?.upgradeMaxParallelAgents) || 4,
			frontendCacheMinutes: Number(settingsStore.current?.frontendCacheMinutes) || 10
		};
	}

	const { formInputs } = untrack(() =>
		createSettingsForm({
			schema: formSchema,
			currentSettings: readFormValues(),
			getCurrentSettings: readFormValues,
			onSave: async (values) => {
				await settingsStore.save(values);
			}
		})
	);
</script>

<SettingsPageLayout title={m.connectivity()} description={m.connectivity_description()} showReadOnlyTag={settingsStore.readOnly}>
	{#snippet mainContent()}
		<fieldset disabled={settingsStore.readOnly} class="contents">
			<SettingsSection>
				<SettingsRow
					for="allEnvironmentsLimit"
					label={m.all_environments_limit()}
					description={m.all_environments_limit_description()}
					helpText={m.all_environments_limit_help()}
					error={formInputs.allEnvironmentsLimit.error}
					lock={settingsStore.lock('allEnvironmentsLimit')}
				>
					{#snippet children({ describedBy, errorId })}
						<Input
							id="allEnvironmentsLimit"
							type="number"
							min="2"
							max="256"
							placeholder="64"
							bind:value={formInputs.allEnvironmentsLimit.value}
							disabled={settingsStore.isLocked('allEnvironmentsLimit')}
							aria-invalid={!!formInputs.allEnvironmentsLimit.error}
							aria-describedby={describedBy}
							aria-errormessage={errorId}
						/>
					{/snippet}
				</SettingsRow>
				<SettingsRow
					for="upgradeMaxParallelAgents"
					label={m.upgrade_max_parallel_agents()}
					description={m.upgrade_max_parallel_agents_description()}
					helpText={m.upgrade_max_parallel_agents_help()}
					error={formInputs.upgradeMaxParallelAgents.error}
					lock={settingsStore.lock('upgradeMaxParallelAgents')}
				>
					{#snippet children({ describedBy, errorId })}
						<Input
							id="upgradeMaxParallelAgents"
							type="number"
							min="1"
							max="16"
							placeholder="4"
							bind:value={formInputs.upgradeMaxParallelAgents.value}
							disabled={settingsStore.isLocked('upgradeMaxParallelAgents')}
							aria-invalid={!!formInputs.upgradeMaxParallelAgents.error}
							aria-describedby={describedBy}
							aria-errormessage={errorId}
						/>
					{/snippet}
				</SettingsRow>
				<SettingsRow
					for="frontendCacheMinutes"
					label={m.cached_data_lifetime()}
					description={m.cached_data_lifetime_description()}
					helpText={m.cached_data_lifetime_help()}
					error={formInputs.frontendCacheMinutes.error}
					lock={settingsStore.lock('frontendCacheMinutes')}
				>
					{#snippet children({ describedBy, errorId })}
						<Input
							id="frontendCacheMinutes"
							type="number"
							min="1"
							max="120"
							placeholder="10"
							bind:value={formInputs.frontendCacheMinutes.value}
							disabled={settingsStore.isLocked('frontendCacheMinutes')}
							aria-invalid={!!formInputs.frontendCacheMinutes.error}
							aria-describedby={describedBy}
							aria-errormessage={errorId}
						/>
					{/snippet}
				</SettingsRow>
			</SettingsSection>
		</fieldset>
	{/snippet}
</SettingsPageLayout>

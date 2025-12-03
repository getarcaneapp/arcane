<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import ZapIcon from '@lucide/svelte/icons/zap';
	import * as Alert from '$lib/components/ui/alert';
	import type { Settings } from '$lib/types/settings.type';
	import { z } from 'zod/v4';
	import { onMount } from 'svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { m } from '$lib/paraglide/messages';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import TrashIcon from '@lucide/svelte/icons/trash';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import settingsStore from '$lib/stores/config-store';
	import BoxesIcon from '@lucide/svelte/icons/boxes';
	import { SettingsPageLayout } from '$lib/layouts';
	import { createSettingsForm } from '$lib/utils/settings-form.util';

	let { data } = $props();
	const currentSettings = $derived<Settings>($settingsStore || data.settings!);
	const isReadOnly = $derived.by(() => $settingsStore?.uiConfigDisabled);

	const formSchema = z.object({
		pollingEnabled: z.boolean(),
		pollingInterval: z.number().int().min(5).max(10080),
		autoUpdate: z.boolean(),
		autoUpdateInterval: z.number().int(),
		dockerPruneMode: z.enum(['all', 'dangling']),
		maxImageUploadSize: z.number().int().min(50).max(5000),
		defaultShell: z.string()
	});

	let pruneMode = $derived(currentSettings.dockerPruneMode);

	type PollingIntervalMode = 'hourly' | 'daily' | 'weekly' | 'custom';

	const imagePollingOptions: Array<{
		value: PollingIntervalMode;
		label: string;
		description: string;
		minutes?: number;
	}> = [
		{ value: 'hourly', minutes: 60, label: m.hourly(), description: m.polling_hourly_description() },
		{ value: 'daily', minutes: 1440, label: m.daily(), description: m.polling_daily_description() },
		{ value: 'weekly', minutes: 10080, label: m.weekly(), description: m.polling_weekly_description() },
		{ value: 'custom', label: m.custom(), description: m.use_custom_polling_value() }
	];

	const presetToMinutes = Object.fromEntries(
		imagePollingOptions.filter((o) => o.value !== 'custom').map((o) => [o.value, o.minutes!])
	) as Record<Exclude<PollingIntervalMode, 'custom'>, number>;

	let pollingIntervalMode = $state<PollingIntervalMode>(
		imagePollingOptions.find((o) => o.minutes === currentSettings.pollingInterval)?.value ?? 'custom'
	);

	const pruneModeOptions = [
		{ value: 'all', label: m.docker_prune_all(), description: m.docker_prune_all_description() },
		{ value: 'dangling', label: m.docker_prune_dangling(), description: m.docker_prune_dangling_description() }
	];

	const pruneModeDescription = $derived(
		pruneModeOptions.find((o) => o.value === pruneMode)?.description ?? m.docker_prune_mode_description()
	);

	const shellOptions = [
		{ value: '/bin/sh', label: '/bin/sh', description: m.docker_shell_sh_description() },
		{ value: '/bin/bash', label: '/bin/bash', description: m.docker_shell_bash_description() },
		{ value: '/bin/ash', label: '/bin/ash', description: m.docker_shell_ash_description() },
		{ value: '/bin/zsh', label: '/bin/zsh', description: m.docker_shell_zsh_description() }
	];

	let shellSelectValue = $state<string>(shellOptions.find((o) => o.value === currentSettings.defaultShell)?.value ?? 'custom');

	let { formInputs, registerOnMount } = $derived(
		createSettingsForm({
			schema: formSchema,
			currentSettings,
			successMessage: m.general_settings_saved()
		})
	);

	// Sync polling mode select with form value
	$effect(() => {
		if (pollingIntervalMode !== 'custom') {
			$formInputs.pollingInterval.value = presetToMinutes[pollingIntervalMode];
		}
	});

	// Sync shell select with form value
	$effect(() => {
		if (shellSelectValue !== 'custom') {
			$formInputs.defaultShell.value = shellSelectValue;
		}
	});

	onMount(() => registerOnMount());
</script>

<SettingsPageLayout
	title={m.docker_title()}
	description={m.docker_description()}
	icon={BoxesIcon}
	pageType="form"
	showReadOnlyTag={isReadOnly}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative">
			<div class="space-y-4 sm:space-y-6">
				<Card.Root>
					<Card.Header icon={ActivityIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.docker_image_polling_title()}</Card.Title>
							<Card.Description>{m.docker_image_polling_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="space-y-3">
							<SwitchWithLabel
								id="pollingEnabled"
								label={m.docker_enable_polling_label()}
								description={m.docker_enable_polling_description()}
								error={$formInputs.pollingEnabled.error}
								bind:checked={$formInputs.pollingEnabled.value}
							/>

							{#if $formInputs.pollingEnabled.value}
								<div class="border-primary/20 space-y-3 border-l-2 pl-3">
									<SelectWithLabel
										id="pollingIntervalMode"
										name="pollingIntervalMode"
										bind:value={pollingIntervalMode}
										label={m.docker_polling_interval_label()}
										placeholder={m.docker_polling_interval_placeholder_select()}
										options={imagePollingOptions.map(({ value, label, description }) => ({ value, label, description }))}
									/>

									{#if pollingIntervalMode === 'custom'}
										<TextInputWithLabel
											bind:value={$formInputs.pollingInterval.value}
											error={$formInputs.pollingInterval.error}
											label={m.custom_polling_interval()}
											placeholder={m.docker_polling_interval_placeholder()}
											helpText={m.docker_polling_interval_description()}
											type="number"
										/>
									{/if}

									{#if $formInputs.pollingInterval.value < 30}
										<Alert.Root variant="warning">
											<ZapIcon class="size-4" />
											<Alert.Title>{m.docker_rate_limit_warning_title()}</Alert.Title>
											<Alert.Description>{m.docker_rate_limit_warning_description()}</Alert.Description>
										</Alert.Root>
									{/if}
								</div>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>

				{#if $formInputs.pollingEnabled.value}
					<Card.Root>
						<Card.Header icon={RefreshCwIcon}>
							<div class="flex flex-col space-y-1.5">
								<Card.Title>{m.docker_auto_updates_title()}</Card.Title>
								<Card.Description>{m.docker_auto_updates_description()}</Card.Description>
							</div>
						</Card.Header>
						<Card.Content class="px-3 py-4 sm:px-6">
							<div class="space-y-3">
								<SwitchWithLabel
									id="autoUpdateSwitch"
									label={m.docker_auto_update_label()}
									description={m.docker_auto_update_description()}
									error={$formInputs.autoUpdate.error}
									bind:checked={$formInputs.autoUpdate.value}
								/>

								{#if $formInputs.autoUpdate.value}
									<div class="border-primary/20 border-l-2 pl-3">
										<TextInputWithLabel
											bind:value={$formInputs.autoUpdateInterval.value}
											error={$formInputs.autoUpdateInterval.error}
											label={m.docker_auto_update_interval_label()}
											placeholder={m.docker_auto_update_interval_placeholder()}
											helpText={m.docker_auto_update_interval_description()}
											type="number"
										/>
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}

				<Card.Root>
					<Card.Header icon={TrashIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.docker_cleanup_settings_title()}</Card.Title>
							<Card.Description>{m.docker_cleanup_settings_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="space-y-3">
							<SelectWithLabel
								id="dockerPruneMode"
								name="pruneMode"
								bind:value={$formInputs.dockerPruneMode.value}
								error={$formInputs.dockerPruneMode.error}
								label={m.docker_prune_action_label()}
								description={pruneModeDescription}
								placeholder={m.docker_prune_placeholder()}
								options={pruneModeOptions}
								onValueChange={(v) => (pruneMode = v as 'all' | 'dangling')}
							/>

							<TextInputWithLabel
								bind:value={$formInputs.maxImageUploadSize.value}
								error={$formInputs.maxImageUploadSize.error}
								label={m.docker_max_upload_size_label()}
								placeholder={m.docker_max_upload_size_placeholder()}
								helpText={m.docker_max_upload_size_description()}
								type="number"
							/>
						</div>
					</Card.Content>
				</Card.Root>

				<Card.Root>
					<Card.Header icon={TerminalIcon}>
						<div class="flex flex-col space-y-1.5">
							<Card.Title>{m.docker_terminal_settings_title()}</Card.Title>
							<Card.Description>{m.docker_terminal_settings_description()}</Card.Description>
						</div>
					</Card.Header>
					<Card.Content class="px-3 py-4 sm:px-6">
						<div class="space-y-3">
							<SelectWithLabel
								id="shellSelectValue"
								name="shellSelectValue"
								bind:value={shellSelectValue}
								label={m.docker_default_shell_label()}
								description={m.docker_default_shell_description()}
								placeholder={m.docker_default_shell_placeholder()}
								options={[
									...shellOptions,
									{ value: 'custom', label: m.custom(), description: m.docker_shell_custom_description() }
								]}
							/>

							{#if shellSelectValue === 'custom'}
								<div class="border-primary/20 border-l-2 pl-3">
									<TextInputWithLabel
										bind:value={$formInputs.defaultShell.value}
										error={$formInputs.defaultShell.error}
										label={m.custom()}
										placeholder={m.docker_shell_custom_path_placeholder()}
										helpText={m.docker_shell_custom_path_help()}
										type="text"
									/>
								</div>
							{/if}
						</div>
					</Card.Content>
				</Card.Root>
			</div>
		</fieldset>
	{/snippet}
</SettingsPageLayout>

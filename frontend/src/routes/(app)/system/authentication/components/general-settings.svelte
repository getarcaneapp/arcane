<script lang="ts" module>
	import type { Settings } from '#lib/types/settings.js';

	export type GeneralSettingsFormValues = Pick<
		Settings,
		| 'authLocalEnabled'
		| 'authSessionTimeout'
		| 'authPasswordMinLength'
		| 'authPasswordRequireUppercase'
		| 'authPasswordRequireLowercase'
		| 'authPasswordRequireNumber'
		| 'authPasswordRequireSymbol'
		| 'hstsEnabled'
	>;
</script>

<script lang="ts">
	import { Input } from '@arcane/ui/input';
	import { Switch } from '@arcane/ui/switch';

	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { FormInputs } from '#lib/types/ui/form.js';

	type PasswordRuleKey =
		| 'authPasswordRequireUppercase'
		| 'authPasswordRequireLowercase'
		| 'authPasswordRequireNumber'
		| 'authPasswordRequireSymbol';

	let {
		formInputs,
		showLocalAuth,
		onLocalAuthChange
	}: {
		formInputs: FormInputs<GeneralSettingsFormValues>;
		showLocalAuth: boolean;
		onLocalAuthChange: (checked: boolean) => void;
	} = $props();

	const passwordRules: { key: PasswordRuleKey; label: string }[] = [
		{
			key: 'authPasswordRequireUppercase',
			label: m.require_uppercase_letter()
		},
		{
			key: 'authPasswordRequireLowercase',
			label: m.require_lowercase_letter()
		},
		{
			key: 'authPasswordRequireNumber',
			label: m.require_number()
		},
		{
			key: 'authPasswordRequireSymbol',
			label: m.require_symbol()
		}
	];
</script>

{#if showLocalAuth}
	<SettingsSection title={m.authentication()}>
		<SettingsRow
			for="localAuthSwitch"
			label={m.security_local_auth_label()}
			description={m.security_local_auth_description()}
			layout="switch"
			error={formInputs.authLocalEnabled.error}
			lock={settingsStore.lock('authLocalEnabled')}
		>
			{#snippet children({ describedBy })}
				<Switch
					id="localAuthSwitch"
					bind:checked={formInputs.authLocalEnabled.value}
					onCheckedChange={onLocalAuthChange}
					aria-describedby={describedBy}
				/>
			{/snippet}
		</SettingsRow>
	</SettingsSection>
{/if}

<SettingsSection title={m.security_session_heading()}>
	<SettingsRow
		for="authSessionTimeout"
		label={m.security_session_timeout_label()}
		description={m.security_session_timeout_description()}
		error={formInputs.authSessionTimeout.error}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id="authSessionTimeout"
				type="number"
				bind:value={formInputs.authSessionTimeout.value}
				aria-invalid={!!formInputs.authSessionTimeout.error}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
	<SettingsRow
		for="hstsEnabledSwitch"
		label={m.hsts_label()}
		description={m.hsts_description()}
		layout="switch"
		lock={settingsStore.lock('hstsEnabled')}
	>
		{#snippet children({ describedBy })}
			<Switch id="hstsEnabledSwitch" bind:checked={formInputs.hstsEnabled.value} aria-describedby={describedBy} />
		{/snippet}
	</SettingsRow>
</SettingsSection>

<SettingsSection title={m.security_password_policy_label()} description={m.security_password_policy_description()}>
	<SettingsRow
		for="authPasswordMinLength"
		label={m.minimum_length()}
		description={m.minimum_length_description()}
		error={formInputs.authPasswordMinLength.error}
		lock={settingsStore.lock('authPasswordMinLength')}
	>
		{#snippet children({ describedBy, errorId })}
			<Input
				id="authPasswordMinLength"
				type="number"
				bind:value={formInputs.authPasswordMinLength.value}
				aria-invalid={!!formInputs.authPasswordMinLength.error}
				aria-describedby={describedBy}
				aria-errormessage={errorId}
			/>
		{/snippet}
	</SettingsRow>
	{#each passwordRules as rule (rule.key)}
		<SettingsRow for="{rule.key}Switch" label={rule.label} layout="switch" lock={settingsStore.lock(rule.key)}>
			{#snippet children({ describedBy })}
				<Switch id="{rule.key}Switch" bind:checked={formInputs[rule.key].value} aria-describedby={describedBy} />
			{/snippet}
		</SettingsRow>
	{/each}
</SettingsSection>

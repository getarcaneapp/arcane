<script lang="ts" module>
	import type { Settings } from '#lib/types/settings.js';

	export type LdapSettingsFormValues = Pick<
		Settings,
		| 'ldapUrl'
		| 'ldapBindDn'
		| 'ldapBase'
		| 'ldapUserSearchFilter'
		| 'ldapUserGroupSearchFilter'
		| 'ldapSkipCertVerify'
		| 'ldapStartTls'
		| 'ldapAttributeUserUniqueIdentifier'
		| 'ldapAttributeUserUsername'
		| 'ldapAttributeUserEmail'
		| 'ldapAttributeUserDisplayName'
		| 'ldapAttributeUserProfilePicture'
		| 'ldapAttributeGroupMember'
		| 'ldapAttributeGroupName'
		| 'ldapSoftDeleteUsers'
		| 'ldapSyncInterval'
	> & {
		ldapBindPassword: string;
		ldapClearBindPassword: boolean;
	};
</script>

<script lang="ts">
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import type { TabItem } from '#lib/components/tab-bar/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import type { FormInputs } from '#lib/types/ui/form.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SettingsTextRows, { type SettingsTextField } from './settings-text-rows.svelte';

	type TextFieldKey = Exclude<
		keyof LdapSettingsFormValues,
		'ldapBindPassword' | 'ldapClearBindPassword' | 'ldapSkipCertVerify' | 'ldapStartTls' | 'ldapSoftDeleteUsers'
	>;

	type TextField = SettingsTextField<TextFieldKey>;

	let {
		formInputs,
		hasStoredBindPassword,
		canSync
	}: {
		formInputs: FormInputs<LdapSettingsFormValues>;
		hasStoredBindPassword: boolean;
		canSync: boolean;
	} = $props();

	let syncing = $state(false);

	const connectionFields: TextField[] = [
		{
			key: 'ldapUrl',
			label: m.server_url(),
			description: m.server_url_description(),
			placeholder: m.server_url_placeholder()
		},
		{
			key: 'ldapBindDn',
			label: m.bind_dn(),
			description: m.bind_dn_description(),
			placeholder: m.bind_dn_placeholder()
		},
		{
			key: 'ldapBase',
			label: m.base_dn(),
			description: m.base_dn_description(),
			placeholder: m.base_dn_placeholder()
		}
	];

	const filterFields: TextField[] = [
		{
			key: 'ldapUserSearchFilter',
			label: m.user_search_filter(),
			description: m.user_search_filter_description(),
			placeholder: m.user_search_filter_placeholder()
		},
		{
			key: 'ldapUserGroupSearchFilter',
			label: m.group_search_filter(),
			description: m.group_search_filter_description(),
			placeholder: m.group_search_filter_placeholder()
		}
	];

	const attributeFields: TextField[] = [
		{
			key: 'ldapAttributeUserUniqueIdentifier',
			label: m.user_unique_identifier(),
			description: m.unique_identifier_description(),
			placeholder: m.unique_identifier_placeholder()
		},
		{
			key: 'ldapAttributeUserUsername',
			label: m.username_attribute(),
			placeholder: m.username_attribute_placeholder()
		},
		{
			key: 'ldapAttributeUserEmail',
			label: m.email_attribute(),
			placeholder: m.email_attribute_placeholder()
		},
		{
			key: 'ldapAttributeUserDisplayName',
			label: m.display_name_attribute(),
			placeholder: m.display_name_attribute_placeholder()
		},
		{
			key: 'ldapAttributeUserProfilePicture',
			label: m.profile_picture_attribute(),
			placeholder: m.profile_picture_attribute_placeholder()
		},
		{
			key: 'ldapAttributeGroupMember',
			label: m.group_member_attribute(),
			placeholder: m.group_member_attribute_placeholder()
		},
		{
			key: 'ldapAttributeGroupName',
			label: m.group_name_attribute(),
			description: m.group_name_attribute_description(),
			placeholder: m.group_name_attribute_placeholder()
		}
	];

	const syncFields: TextField[] = [
		{
			key: 'ldapSyncInterval',
			label: m.sync_schedule(),
			description: m.sync_schedule_description(),
			placeholder: m.sync_schedule_placeholder()
		}
	];

	let section = $state('connection');

	const sections: { value: string; label: string; keys: (keyof LdapSettingsFormValues)[] }[] = [
		{ value: 'connection', label: m.connection(), keys: [...connectionFields.map((field) => field.key), 'ldapBindPassword'] },
		{ value: 'filters', label: m.filters(), keys: filterFields.map((field) => field.key) },
		{ value: 'attributes', label: m.attributes(), keys: attributeFields.map((field) => field.key) },
		{ value: 'sync', label: m.sync(), keys: syncFields.map((field) => field.key) }
	];

	const sectionItems: TabItem[] = $derived(
		sections.map((item) => ({
			value: item.value,
			label: item.label,
			badge: item.keys.filter((key) => formInputs[key].error).length || undefined
		}))
	);

	async function syncNow() {
		syncing = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.syncLdap()),
			message: m.ldap_sync_failed(),
			setLoadingState: (value) => (syncing = value),
			onSuccess: () => {
				toast.success(m.ldap_sync_finished());
			}
		});
	}
</script>

<Tabs.Root bind:value={section}>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<Tabs.List>
			{#each sectionItems as item (item.value)}
				<Tabs.Trigger value={item.value}>
					{item.label}
					{#if item.badge !== undefined}
						<Badge variant="red">{item.badge}</Badge>
					{/if}
				</Tabs.Trigger>
			{/each}
		</Tabs.List>
		{#if canSync}
			<ArcaneButton action="sync" customLabel={m.sync_now()} loading={syncing} disabled={syncing} onclick={syncNow} />
		{/if}
	</div>

	<Tabs.Content value="connection" class="mt-4">
		<SettingsSection>
			<SettingsTextRows fields={connectionFields} {formInputs} />
			<SettingsRow
				for="ldapBindPassword"
				label={m.bind_password()}
				helpText={m.bind_password_help()}
				error={formInputs.ldapBindPassword.error}
				lock={settingsStore.lock('ldapBindPassword')}
			>
				{#snippet children({ describedBy, errorId })}
					<Input
						id="ldapBindPassword"
						type="password"
						placeholder={m.bind_password_placeholder()}
						disabled={formInputs.ldapClearBindPassword.value}
						bind:value={formInputs.ldapBindPassword.value}
						aria-invalid={!!formInputs.ldapBindPassword.error}
						aria-describedby={describedBy}
						aria-errormessage={errorId}
					/>
				{/snippet}
			</SettingsRow>
			{#if hasStoredBindPassword && !settingsStore.isLocked('ldapBindPassword')}
				<SettingsRow for="ldapClearBindPassword" label={m.clear_stored_bind_password()} layout="switch">
					{#snippet children({ describedBy })}
						<Switch
							id="ldapClearBindPassword"
							bind:checked={formInputs.ldapClearBindPassword.value}
							aria-describedby={describedBy}
						/>
					{/snippet}
				</SettingsRow>
			{/if}
			<SettingsRow
				for="ldapStartTls"
				label={m.use_starttls()}
				description={m.use_starttls_description()}
				layout="switch"
				lock={settingsStore.lock('ldapStartTls')}
			>
				{#snippet children({ describedBy })}
					<Switch id="ldapStartTls" bind:checked={formInputs.ldapStartTls.value} aria-describedby={describedBy} />
				{/snippet}
			</SettingsRow>
			<SettingsRow
				for="ldapSkipCertVerify"
				label={m.skip_certificate_verification()}
				description={m.skip_certificate_verification_description()}
				layout="switch"
				lock={settingsStore.lock('ldapSkipCertVerify')}
			>
				{#snippet children({ describedBy })}
					<Switch id="ldapSkipCertVerify" bind:checked={formInputs.ldapSkipCertVerify.value} aria-describedby={describedBy} />
				{/snippet}
			</SettingsRow>
		</SettingsSection>
	</Tabs.Content>

	<Tabs.Content value="filters" class="mt-4">
		<SettingsSection>
			<SettingsTextRows fields={filterFields} {formInputs} />
		</SettingsSection>
	</Tabs.Content>

	<Tabs.Content value="attributes" class="mt-4">
		<SettingsSection>
			<SettingsTextRows fields={attributeFields} {formInputs} />
		</SettingsSection>
	</Tabs.Content>

	<Tabs.Content value="sync" class="mt-4">
		<SettingsSection>
			<SettingsTextRows fields={syncFields} {formInputs} />
			<SettingsRow
				for="ldapSoftDeleteUsers"
				label={m.keep_disabled_users()}
				description={m.keep_disabled_users_description()}
				layout="switch"
				lock={settingsStore.lock('ldapSoftDeleteUsers')}
			>
				{#snippet children({ describedBy })}
					<Switch id="ldapSoftDeleteUsers" bind:checked={formInputs.ldapSoftDeleteUsers.value} aria-describedby={describedBy} />
				{/snippet}
			</SettingsRow>
		</SettingsSection>
	</Tabs.Content>
</Tabs.Root>

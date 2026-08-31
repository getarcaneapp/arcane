<script lang="ts">
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';
	import PasskeySettings from '#lib/components/auth/passkey-settings.svelte';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { LogoutIcon, ShieldAlertIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { userService } from '#lib/services/user-service';
	import settingsStore from '#lib/stores/config-store';
	import userStore from '#lib/stores/user-store';

	const currentUser = $derived($userStore);
	const isOidcUser = $derived(Boolean(currentUser?.oidcSubjectId));
	const autoLogin = fromStore(settingsStore.autoLoginEnabled);
	const autoLoginEnabled = $derived(autoLogin.current);

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let passwordSaving = $state(false);
	let revokingAll = $state(false);
	const passwordValid = $derived(currentPassword.length > 0 && newPassword.length >= 8 && newPassword === confirmPassword);

	async function changePassword() {
		if (!passwordValid || passwordSaving) return;
		passwordSaving = true;
		try {
			await userService.changePassword({ currentPassword, newPassword });
			toast.success(m.account_password_updated());
			currentPassword = '';
			newPassword = '';
			confirmPassword = '';
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_update_failed({ resource: m.common_password() }));
		} finally {
			passwordSaving = false;
		}
	}

	async function logoutAllOther() {
		if (revokingAll) return;
		revokingAll = true;
		try {
			await userService.logoutAllOtherSessions();
			toast.success(m.account_sessions_signed_out());
		} catch (error) {
			toast.error(error instanceof Error ? error.message : m.common_action_failed());
		} finally {
			revokingAll = false;
		}
	}
</script>

{#if !isOidcUser}
	<section class="space-y-5">
		<div>
			<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.common_password()}</h2>
			<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_password_desc()}</p>
		</div>
		<TextInputWithLabel
			id="account-current-password"
			type="password"
			bind:value={currentPassword}
			label={m.account_current_password()}
			autocomplete="current-password"
		/>
		<div class="grid gap-5 sm:grid-cols-2">
			<TextInputWithLabel
				id="account-new-password"
				type="password"
				bind:value={newPassword}
				label={m.account_new_password()}
				helpText={m.account_password_min_length()}
				autocomplete="new-password"
			/>
			<TextInputWithLabel
				id="account-confirm-password"
				type="password"
				bind:value={confirmPassword}
				label={m.account_confirm_password()}
				error={confirmPassword.length > 0 && confirmPassword !== newPassword ? m.account_passwords_dont_match() : null}
				autocomplete="new-password"
			/>
		</div>
		<div class="flex justify-end">
			<ArcaneButton
				action="save"
				customLabel={m.account_update_password()}
				onclick={changePassword}
				loading={passwordSaving}
				disabled={!passwordValid || passwordSaving}
			/>
		</div>
	</section>
{/if}

<PasskeySettings />

{#if !autoLoginEnabled}
	<section class="space-y-5">
		<div class="flex items-center gap-2">
			<ShieldAlertIcon class="size-4 text-destructive" />
			<div>
				<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_danger_zone()}</h2>
				<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_danger_zone_desc()}</p>
			</div>
		</div>
		<div class="rounded-xl border border-destructive/30 bg-destructive/5 p-4">
			<div class="grid gap-4 sm:grid-cols-2">
				<div class="space-y-2">
					<div class="text-sm font-medium">{m.account_signout_other()}</div>
					<p class="text-xs text-muted-foreground">{m.account_signout_other_desc()}</p>
					<ArcaneButton
						action="restart"
						tone="outline"
						size="sm"
						customLabel={m.account_signout_other()}
						onclick={logoutAllOther}
						loading={revokingAll}
						disabled={revokingAll}
					/>
				</div>

				<div class="space-y-2">
					<div class="text-sm font-medium">{m.common_log_out()}</div>
					<p class="text-xs text-muted-foreground">{m.account_signout_this()}</p>
					<form action="/logout" method="POST">
						<ArcaneButton
							action="cancel"
							tone="outline"
							size="sm"
							customLabel={m.common_log_out()}
							icon={LogoutIcon}
							type="submit"
							class="border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive"
						/>
					</form>
				</div>
			</div>
		</div>
	</section>
{/if}

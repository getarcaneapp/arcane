<script lang="ts">
	import { onMount } from 'svelte';
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';
	import { mode, toggleMode } from 'mode-watcher';
	import { format, formatDistanceToNow } from 'date-fns';
	import HeaderCard from '$lib/components/header-card.svelte';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Separator } from '$lib/components/ui/separator';
	import * as Avatar from '$lib/components/ui/avatar';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import { userService } from '$lib/services/user-service';
	import { apiKeyService } from '$lib/services/api-key-service';
	import userStore from '$lib/stores/user-store';
	import settingsStore from '$lib/stores/config-store';
	import { getDefaultProfilePicture } from '$lib/utils/docker';
	import { GLOBAL_SCOPE } from '$lib/types/auth';
	import type { ApiKey, ApiKeyCreated } from '$lib/types/auth';
	import { UserIcon, LogoutIcon, ShieldAlertIcon, SunIcon, MoonIcon, ApiKeyIcon, AddIcon, CopyIcon, TrashIcon } from '$lib/icons';

	let { data: _data }: PageProps = $props();

	const BUILT_IN_ROLE_LABELS: Record<string, string> = {
		role_admin: 'Administrator',
		role_editor: 'Editor',
		role_deployer: 'Deployer',
		role_viewer: 'Viewer'
	};

	function prettyRoleName(roleId: string): string {
		return BUILT_IN_ROLE_LABELS[roleId] ?? roleId.replace(/^role_/, '').replace(/_/g, ' ');
	}

	function safeFormatDate(input: string | undefined, fmt: string): string | null {
		if (!input) return null;
		try {
			return format(new Date(input), fmt);
		} catch {
			return null;
		}
	}

	function safeFormatRelative(input: string | undefined): string | null {
		if (!input) return null;
		try {
			return formatDistanceToNow(new Date(input), { addSuffix: true });
		} catch {
			return null;
		}
	}

	const currentUser = $derived($userStore);
	const isOidcUser = $derived(Boolean(currentUser?.oidcSubjectId));

	const settings = fromStore(settingsStore);
	const autoLogin = fromStore(settingsStore.autoLoginEnabled);
	const autoLoginEnabled = $derived(autoLogin.current);
	const gravatarEnabled = $derived(Boolean(settings.current?.enableGravatar));

	let profileDisplayName = $state('');
	let profileEmail = $state('');
	let profileSaving = $state(false);
	let profileLoaded = $state(false);

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let passwordSaving = $state(false);

	let revokingAll = $state(false);
	let avatarUrl = $state<string>(getDefaultProfilePicture());

	let apiKeys = $state<ApiKey[]>([]);
	let apiKeysLoading = $state(false);
	let showCreateKeyForm = $state(false);
	let newKeyName = $state('');
	let newKeyDescription = $state('');
	let creatingKey = $state(false);
	let createdKey = $state<ApiKeyCreated | null>(null);

	$effect(() => {
		if (!profileLoaded && currentUser) {
			profileDisplayName = currentUser.displayName ?? '';
			profileEmail = currentUser.email ?? '';
			profileLoaded = true;
		}
	});

	$effect(() => {
		void updateAvatar(currentUser?.email, gravatarEnabled);
	});

	const profileDirty = $derived(
		profileDisplayName.trim() !== (currentUser?.displayName ?? '') || profileEmail.trim() !== (currentUser?.email ?? '')
	);

	const passwordValid = $derived(currentPassword.length > 0 && newPassword.length >= 8 && newPassword === confirmPassword);

	async function updateAvatar(email: string | undefined, enabled: boolean) {
		if (!enabled || !email) {
			avatarUrl = getDefaultProfilePicture();
			return;
		}
		try {
			const encoder = new TextEncoder();
			const data = encoder.encode(email.toLowerCase().trim());
			const hashBuffer = await crypto.subtle.digest('SHA-256', data);
			const hash = Array.from(new Uint8Array(hashBuffer))
				.map((b) => b.toString(16).padStart(2, '0'))
				.join('');
			avatarUrl = `https://www.gravatar.com/avatar/${hash}?s=128`;
		} catch {
			avatarUrl = getDefaultProfilePicture();
		}
	}

	async function saveProfile() {
		if (!currentUser || !profileDirty || profileSaving) return;
		profileSaving = true;
		try {
			const updated = await userService.updateMyProfile({
				displayName: profileDisplayName.trim(),
				email: profileEmail.trim()
			});
			await userStore.setUser(updated);
			toast.success('Profile updated');
		} catch (err) {
			const msg = err instanceof Error ? err.message : 'Failed to update profile';
			toast.error(msg);
		} finally {
			profileSaving = false;
		}
	}

	function resetProfile() {
		profileDisplayName = currentUser?.displayName ?? '';
		profileEmail = currentUser?.email ?? '';
	}

	async function changePassword() {
		if (!passwordValid || passwordSaving) return;
		passwordSaving = true;
		try {
			await userService.changePassword({ currentPassword, newPassword });
			toast.success('Password updated');
			currentPassword = '';
			newPassword = '';
			confirmPassword = '';
		} catch (err) {
			const msg = err instanceof Error ? err.message : 'Failed to update password';
			toast.error(msg);
		} finally {
			passwordSaving = false;
		}
	}

	async function loadApiKeys() {
		apiKeysLoading = true;
		try {
			apiKeys = await apiKeyService.listMine();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to load API keys');
		} finally {
			apiKeysLoading = false;
		}
	}

	async function createApiKey() {
		if (!newKeyName.trim() || creatingKey) return;
		creatingKey = true;
		try {
			const created = await apiKeyService.createMine({
				name: newKeyName.trim(),
				description: newKeyDescription.trim() || undefined,
				permissions: []
			});
			createdKey = created;
			newKeyName = '';
			newKeyDescription = '';
			showCreateKeyForm = false;
			await loadApiKeys();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to create API key');
		} finally {
			creatingKey = false;
		}
	}

	async function deleteApiKey(id: string, name: string) {
		if (!confirm(`Delete API key "${name}"? This cannot be undone.`)) return;
		try {
			await apiKeyService.deleteMine(id);
			toast.success('API key deleted');
			await loadApiKeys();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to delete API key');
		}
	}

	function copyKeyToClipboard(key: string) {
		void navigator.clipboard.writeText(key);
		toast.success('Key copied to clipboard');
	}

	onMount(() => {
		void loadApiKeys();
	});

	async function logoutAllOther() {
		if (revokingAll) return;
		revokingAll = true;
		try {
			await userService.logoutAllOtherSessions();
			toast.success('All other sessions signed out');
		} catch (err) {
			const msg = err instanceof Error ? err.message : 'Failed to sign out other sessions';
			toast.error(msg);
		} finally {
			revokingAll = false;
		}
	}
</script>

<div class="space-y-6 pb-5 md:space-y-8 md:pb-5">
	<HeaderCard>
		<div class="flex items-center justify-between gap-4">
			<div class="flex min-w-0 flex-1 items-center gap-3 sm:gap-4">
				<div
					class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
				>
					<UserIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0">
					<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">Account</h1>
					<p class="text-muted-foreground mt-1 text-sm">Manage your profile, password, and active sessions</p>
				</div>
			</div>
		</div>
	</HeaderCard>

	{#if currentUser}
		<div class="grid gap-6 lg:grid-cols-3">
			<!-- Left column: profile + password + preferences -->
			<div class="space-y-6 lg:col-span-2">
				<!-- Profile -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">Profile</h2>
						<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Update your display name and email</p>
					</div>
					<div class="space-y-5 p-4 sm:p-6">
						<div class="flex items-center gap-4">
							<Avatar.Root class="size-16 rounded-xl">
								<Avatar.Image src={avatarUrl} alt={currentUser.displayName ?? currentUser.username} />
								<Avatar.Fallback
									class="from-primary/20 to-primary/10 text-primary border-primary/20 rounded-xl border bg-linear-to-br text-xl font-semibold"
								>
									{(currentUser.displayName ?? currentUser.username).charAt(0).toUpperCase()}
								</Avatar.Fallback>
							</Avatar.Root>
							<div class="min-w-0">
								<div class="text-sm font-medium">@{currentUser.username}</div>
								<div class="text-muted-foreground text-xs">
									{isOidcUser ? 'Single sign-on account' : 'Local account'}
								</div>
							</div>
						</div>

						<div class="grid gap-4 sm:grid-cols-2">
							<div class="space-y-2">
								<Label for="account-display-name">Display Name</Label>
								<Input id="account-display-name" bind:value={profileDisplayName} placeholder="Your name" disabled={isOidcUser} />
							</div>
							<div class="space-y-2">
								<Label for="account-email">Email</Label>
								<Input
									id="account-email"
									type="email"
									bind:value={profileEmail}
									placeholder="you@example.com"
									disabled={isOidcUser}
								/>
							</div>
						</div>

						{#if isOidcUser}
							<p class="text-muted-foreground text-xs">Profile fields are managed by your identity provider.</p>
						{:else}
							<div class="flex flex-col-reverse items-stretch justify-end gap-2 sm:flex-row sm:items-center">
								<ArcaneButton
									action="cancel"
									tone="ghost"
									customLabel="Reset"
									onclick={resetProfile}
									disabled={!profileDirty || profileSaving}
								/>
								<ArcaneButton
									action="save"
									customLabel="Save changes"
									onclick={saveProfile}
									loading={profileSaving}
									disabled={!profileDirty || profileSaving}
								/>
							</div>
						{/if}
					</div>
				</Card>

				<!-- Password -->
				{#if !isOidcUser && !autoLoginEnabled}
					<Card class="overflow-hidden">
						<div class="border-b p-4 sm:p-6">
							<h2 class="text-base font-semibold tracking-tight sm:text-lg">Password</h2>
							<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Changing your password signs out every other session</p>
						</div>
						<div class="space-y-4 p-4 sm:p-6">
							<div class="space-y-2">
								<Label for="account-current-password">Current password</Label>
								<Input
									id="account-current-password"
									type="password"
									bind:value={currentPassword}
									autocomplete="current-password"
								/>
							</div>
							<div class="grid gap-4 sm:grid-cols-2">
								<div class="space-y-2">
									<Label for="account-new-password">New password</Label>
									<Input id="account-new-password" type="password" bind:value={newPassword} autocomplete="new-password" />
									<p class="text-muted-foreground text-xs">At least 8 characters</p>
								</div>
								<div class="space-y-2">
									<Label for="account-confirm-password">Confirm new password</Label>
									<Input id="account-confirm-password" type="password" bind:value={confirmPassword} autocomplete="new-password" />
									{#if confirmPassword.length > 0 && confirmPassword !== newPassword}
										<p class="text-destructive text-xs">Passwords don't match</p>
									{/if}
								</div>
							</div>
							<div class="flex justify-end">
								<ArcaneButton
									action="save"
									customLabel="Update password"
									onclick={changePassword}
									loading={passwordSaving}
									disabled={!passwordValid || passwordSaving}
								/>
							</div>
						</div>
					</Card>
				{/if}

				<!-- Preferences -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">Preferences</h2>
						<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Personal display preferences</p>
					</div>
					<div class="divide-y p-2">
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">Theme</div>
								<div class="text-muted-foreground text-xs">Switch between light and dark mode</div>
							</div>
							<button
								type="button"
								onclick={toggleMode}
								class="border-border hover:bg-muted/60 flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors"
							>
								{#if mode.current === 'dark'}
									<MoonIcon class="size-4" />
									Dark
								{:else}
									<SunIcon class="size-4" />
									Light
								{/if}
							</button>
						</div>
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">Language</div>
								<div class="text-muted-foreground text-xs">UI language for this account</div>
							</div>
							<LocalePicker inline />
						</div>
					</div>
				</Card>

				<!-- API keys -->
				<Card class="overflow-hidden">
					<div class="flex items-start justify-between gap-3 border-b p-4 sm:p-6">
						<div class="min-w-0">
							<h2 class="text-base font-semibold tracking-tight sm:text-lg">API keys</h2>
							<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Personal tokens for programmatic access</p>
						</div>
						{#if !showCreateKeyForm && !createdKey}
							<ArcaneButton
								action="create"
								tone="outline"
								size="sm"
								customLabel="New key"
								icon={AddIcon}
								onclick={() => (showCreateKeyForm = true)}
							/>
						{/if}
					</div>

					<div class="p-4 sm:p-6">
						{#if createdKey}
							<div class="border-primary/30 bg-primary/5 mb-4 space-y-3 rounded-lg border p-4">
								<div>
									<div class="text-sm font-semibold">Key created: {createdKey.name}</div>
									<p class="text-muted-foreground mt-1 text-xs">Copy this token now &mdash; you won't be able to see it again.</p>
								</div>
								<div class="flex items-center gap-2">
									<code class="bg-background flex-1 truncate rounded border px-3 py-2 font-mono text-xs">
										{createdKey.key}
									</code>
									<ArcaneButton
										action="base"
										tone="outline"
										size="sm"
										customLabel="Copy"
										icon={CopyIcon}
										onclick={() => copyKeyToClipboard(createdKey!.key)}
									/>
								</div>
								<div class="flex justify-end">
									<ArcaneButton
										action="cancel"
										tone="ghost"
										size="sm"
										customLabel="I've saved it"
										onclick={() => (createdKey = null)}
									/>
								</div>
							</div>
						{/if}

						{#if showCreateKeyForm}
							<div class="mb-4 space-y-3 rounded-lg border p-4">
								<div class="space-y-2">
									<Label for="api-key-name">Key name</Label>
									<Input id="api-key-name" bind:value={newKeyName} placeholder="e.g. CI deploy bot" />
								</div>
								<div class="space-y-2">
									<Label for="api-key-description">Description (optional)</Label>
									<Input id="api-key-description" bind:value={newKeyDescription} placeholder="What is this key for?" />
								</div>
								<p class="text-muted-foreground text-xs">
									Keys are created without explicit permission scopes. An admin can add scopes later from Settings &rarr; API Keys
									if needed.
								</p>
								<div class="flex justify-end gap-2">
									<ArcaneButton
										action="cancel"
										tone="ghost"
										customLabel="Cancel"
										onclick={() => {
											showCreateKeyForm = false;
											newKeyName = '';
											newKeyDescription = '';
										}}
										disabled={creatingKey}
									/>
									<ArcaneButton
										action="create"
										customLabel="Create key"
										onclick={createApiKey}
										loading={creatingKey}
										disabled={!newKeyName.trim() || creatingKey}
									/>
								</div>
							</div>
						{/if}

						{#if apiKeysLoading && apiKeys.length === 0}
							<div class="text-muted-foreground py-8 text-center text-sm">Loading keys…</div>
						{:else if apiKeys.length === 0}
							<div class="text-muted-foreground py-8 text-center text-sm">
								<ApiKeyIcon class="mx-auto mb-2 size-8 opacity-40" />
								No API keys yet.
							</div>
						{:else}
							<ul class="divide-y">
								{#each apiKeys as key (key.id)}
									<li class="flex items-center justify-between gap-3 py-3 first:pt-0 last:pb-0">
										<div class="min-w-0 flex-1">
											<div class="flex items-center gap-2">
												<span class="truncate text-sm font-medium">{key.name}</span>
												<code class="text-muted-foreground bg-muted/40 rounded px-1.5 py-0.5 font-mono text-xs">
													{key.keyPrefix}…
												</code>
											</div>
											{#if key.description}
												<div class="text-muted-foreground mt-0.5 truncate text-xs">{key.description}</div>
											{/if}
											<div class="text-muted-foreground mt-1 text-xs">
												{#if safeFormatDate(key.createdAt, 'PP')}
													Created {safeFormatDate(key.createdAt, 'PP')}
												{/if}
												{#if key.lastUsedAt && safeFormatRelative(key.lastUsedAt)}
													· Last used {safeFormatRelative(key.lastUsedAt)}
												{:else}
													· Never used
												{/if}
											</div>
										</div>
										<ArcaneButton
											action="remove"
											tone="ghost"
											size="sm"
											icon={TrashIcon}
											customLabel="Delete"
											showLabel={false}
											class="text-muted-foreground hover:text-destructive hover:bg-destructive/10"
											onclick={() => deleteApiKey(key.id, key.name)}
										/>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				</Card>
			</div>

			<!-- Right column: account info + roles + danger zone -->
			<div class="space-y-6">
				<!-- Account info -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">Account info</h2>
					</div>
					<dl class="divide-y p-2 text-sm">
						<div class="flex items-center justify-between gap-4 p-3">
							<dt class="text-muted-foreground">Username</dt>
							<dd class="font-medium tabular-nums">@{currentUser.username}</dd>
						</div>
						<div class="flex items-center justify-between gap-4 p-3">
							<dt class="text-muted-foreground">Account type</dt>
							<dd class="font-medium">{isOidcUser ? 'Single sign-on' : 'Local'}</dd>
						</div>
						{#if safeFormatDate(currentUser.createdAt, 'PP')}
							<div class="flex items-center justify-between gap-4 p-3">
								<dt class="text-muted-foreground">Member since</dt>
								<dd class="font-medium">{safeFormatDate(currentUser.createdAt, 'PP')}</dd>
							</div>
						{/if}
						<div class="flex items-center justify-between gap-4 p-3">
							<dt class="text-muted-foreground">Last login</dt>
							<dd class="text-right font-medium" title={currentUser.lastLogin ?? ''}>
								{safeFormatRelative(currentUser.lastLogin) ?? 'Never'}
							</dd>
						</div>
					</dl>
				</Card>

				<!-- Roles & access -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">Roles &amp; access</h2>
						<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Your assigned roles</p>
					</div>
					<div class="p-4 sm:p-6">
						{#if currentUser.roleAssignments && currentUser.roleAssignments.length > 0}
							<ul class="space-y-2">
								{#each currentUser.roleAssignments as ra (`${ra.roleId}-${ra.environmentId ?? 'global'}`)}
									<li class="bg-muted/30 flex items-center justify-between gap-3 rounded-lg px-3 py-2">
										<div class="min-w-0">
											<div class="text-sm font-medium">{prettyRoleName(ra.roleId)}</div>
											<div class="text-muted-foreground text-xs">
												{ra.environmentId ? `Environment: ${ra.environmentId}` : 'Global scope'}
												{#if ra.source === 'oidc'}
													<span class="ml-1 opacity-70">· via SSO</span>
												{/if}
											</div>
										</div>
									</li>
								{/each}
							</ul>
						{:else}
							<p class="text-muted-foreground text-sm">No roles assigned.</p>
						{/if}

						{#if currentUser.permissionsByEnv}
							{@const envCount = Object.keys(currentUser.permissionsByEnv).length}
							{@const globalCount = currentUser.permissionsByEnv[GLOBAL_SCOPE]?.length ?? 0}
							<p class="text-muted-foreground mt-3 text-xs">
								{globalCount} global permission{globalCount === 1 ? '' : 's'} across {envCount} environment{envCount === 1
									? ''
									: 's'}.
							</p>
						{/if}
					</div>
				</Card>

				{#if !autoLoginEnabled}
					<Card class="border-destructive/30 overflow-hidden">
						<div class="border-destructive/20 border-b p-4 sm:p-6">
							<div class="flex items-center gap-2">
								<ShieldAlertIcon class="text-destructive size-5" />
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">Danger zone</h2>
							</div>
							<p class="text-muted-foreground mt-1 text-xs sm:text-sm">Session-level actions that affect every device</p>
						</div>
						<div class="space-y-4 p-4 sm:p-6">
							<div class="space-y-2">
								<div class="text-sm font-medium">Sign out other sessions</div>
								<p class="text-muted-foreground text-xs">
									Revokes every active session except this one. Useful if you forgot to log out somewhere.
								</p>
								<ArcaneButton
									action="restart"
									tone="outline"
									customLabel="Sign out other sessions"
									onclick={logoutAllOther}
									loading={revokingAll}
									disabled={revokingAll}
								/>
							</div>

							<Separator />

							<div class="space-y-2">
								<div class="text-sm font-medium">Log out</div>
								<p class="text-muted-foreground text-xs">Sign out of this device.</p>
								<form action="/logout" method="POST">
									<ArcaneButton
										action="cancel"
										tone="outline"
										customLabel="Log out"
										icon={LogoutIcon}
										type="submit"
										class="text-destructive border-destructive/30 hover:bg-destructive/10 hover:text-destructive"
									/>
								</form>
							</div>
						</div>
					</Card>
				{/if}
			</div>
		</div>
	{:else}
		<div class="text-muted-foreground py-12 text-center text-sm">Loading account…</div>
	{/if}
</div>

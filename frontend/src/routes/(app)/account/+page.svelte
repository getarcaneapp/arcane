<script lang="ts">
	import { onMount } from 'svelte';
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';
	import ThemeModeSelector from '$lib/components/theme-mode/theme-mode-selector.svelte';
	import { format, formatDistanceToNow } from 'date-fns';
	import HeaderCard from '$lib/components/header-card.svelte';
	import ApiKeyFormSheet from '$lib/components/sheets/api-key-form-sheet.svelte';
	import { Card } from '$lib/components/ui/card';
	import { Separator } from '$lib/components/ui/separator';
	import * as Avatar from '$lib/components/ui/avatar';
	import * as ImageCropper from '$lib/components/ui/image-cropper';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import LocalePicker from '$lib/components/locale-picker.svelte';
	import TimeFormatPicker from '$lib/components/time-format-picker.svelte';
	import FontSizePicker from '$lib/components/font-size-picker.svelte';
	import { m } from '$lib/paraglide/messages';
	import { userService } from '$lib/services/user-service';
	import { apiKeyService } from '$lib/services/api-key-service';
	import userStore from '$lib/stores/user-store';
	import settingsStore from '$lib/stores/config-store';
	import { getDefaultProfilePicture } from '$lib/utils/docker';
	import { avatarUploadLimitBytes, prepareAvatarUploadFile } from '$lib/utils/avatar-upload';
	import { cn } from '$lib/utils';
	import { GLOBAL_SCOPE } from '$lib/types/auth';
	import type { ApiKey, ApiKeyCreated, ApiKeyPermissionGrant, CreateUserApiKey } from '$lib/types/auth';
	import { UserIcon, LogoutIcon, ShieldAlertIcon, ApiKeyIcon, AddIcon, CopyIcon, TrashIcon } from '$lib/icons';

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
	const avatarMaxUploadSizeMb = $derived(
		Number(settings.current?.avatarMaxUploadSizeMb) > 0 ? Number(settings.current?.avatarMaxUploadSizeMb) : 2
	);
	const avatarMaxUploadSizeBytes = $derived(avatarUploadLimitBytes(avatarMaxUploadSizeMb));

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
	let avatarCacheBuster = $state(Date.now());
	const avatarSrc = $derived(currentUser?.avatarUrl ? `${currentUser.avatarUrl}?t=${avatarCacheBuster}` : '');
	let cropperAvatarSrc = $derived(avatarSrc || avatarUrl);

	let apiKeys = $state<ApiKey[]>([]);
	let apiKeysLoading = $state(false);
	let showCreateKeyForm = $state(false);
	let creatingKey = $state(false);
	let createdKey = $state<ApiKeyCreated | null>(null);

	let avatarUploading = $state(false);

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
			avatarUrl = `https://www.gravatar.com/avatar/${hash}?s=128&d=404`;
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
			toast.success(m.account_profile_updated());
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

	async function handleCroppedAvatar(url: string) {
		avatarUploading = true;
		try {
			const preparedFile = await prepareAvatarUploadFile(url, avatarMaxUploadSizeBytes, ImageCropper.getFileFromUrl);
			if (!preparedFile.ok) {
				toast.error(m.account_avatar_size_error({ maxSizeMb: avatarMaxUploadSizeMb }));
				return;
			}

			const updatedUser = await userService.uploadMyAvatar(preparedFile.file);
			await userStore.setUser(updatedUser);
			avatarCacheBuster = Date.now();
			toast.success(m.account_avatar_upload_success());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.account_avatar_upload_failed());
		} finally {
			avatarUploading = false;
			URL.revokeObjectURL(url);
			if (cropperAvatarSrc === url) cropperAvatarSrc = avatarSrc || avatarUrl;
		}
	}

	function handleUnsupportedAvatarFile() {
		toast.error(m.account_avatar_unsupported_file());
	}

	function handleAvatarCropError() {
		toast.error(m.account_avatar_crop_failed());
	}

	async function removeAvatar() {
		if (!currentUser?.avatarUrl) return;
		avatarUploading = true;
		try {
			const updatedUser = await userService.deleteMyAvatar();
			await userStore.setUser(updatedUser);
			avatarCacheBuster = Date.now();
			toast.success(m.account_avatar_remove_success());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.account_avatar_remove_failed());
		} finally {
			avatarUploading = false;
		}
	}

	async function changePassword() {
		if (!passwordValid || passwordSaving) return;
		passwordSaving = true;
		try {
			await userService.changePassword({ currentPassword, newPassword });
			toast.success(m.account_password_updated());
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

	async function createApiKey({
		apiKey
	}: {
		apiKey: { name: string; description?: string; expiresAt?: string; permissions?: ApiKeyPermissionGrant[] };
		isEditMode: boolean;
		apiKeyId?: string;
	}) {
		creatingKey = true;
		try {
			// Personal keys carry no grants; they inherit the owner's role permissions.
			const payload: CreateUserApiKey = {
				name: apiKey.name,
				description: apiKey.description,
				expiresAt: apiKey.expiresAt
			};
			const created = await apiKeyService.createMine(payload);
			createdKey = created;
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
			toast.success(m.account_api_key_deleted());
			await loadApiKeys();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to delete API key');
		}
	}

	function copyKeyToClipboard(key: string) {
		void navigator.clipboard.writeText(key);
		toast.success(m.common_key_copied());
	}

	onMount(() => {
		void loadApiKeys();
	});

	async function logoutAllOther() {
		if (revokingAll) return;
		revokingAll = true;
		try {
			await userService.logoutAllOtherSessions();
			toast.success(m.account_sessions_signed_out());
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
					class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary ring-1 ring-primary/20 sm:size-10"
				>
					<UserIcon class="size-4 sm:size-5" />
				</div>
				<div class="min-w-0">
					<h1 class="text-2xl font-semibold tracking-tight sm:text-3xl">{m.common_account()}</h1>
					<p class="mt-1 text-sm text-muted-foreground">{m.account_subtitle()}</p>
				</div>
			</div>
		</div>
	</HeaderCard>

	{#if currentUser}
		<div class="grid gap-6 lg:grid-cols-3">
			<!-- Left column: profile + password + API keys -->
			<div class="space-y-6 lg:col-span-2">
				<!-- Profile -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_profile_title()}</h2>
						<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_profile_description()}</p>
					</div>
					<div class="space-y-5 p-4 sm:p-6">
						<ImageCropper.Root
							id="account-avatar-cropper"
							bind:src={cropperAvatarSrc}
							accept="image/png, image/jpeg, image/webp"
							onCropped={handleCroppedAvatar}
							onError={handleAvatarCropError}
							onUnsupportedFile={handleUnsupportedAvatarFile}
						>
							<ImageCropper.Dialog>
								<div class="space-y-1">
									<h3 class="text-base font-semibold tracking-tight">{m.account_avatar_crop_title()}</h3>
									<p class="text-sm text-muted-foreground">{m.account_avatar_crop_description()}</p>
								</div>
								<div class="h-72 overflow-hidden rounded-lg border bg-muted/40">
									<ImageCropper.Cropper />
								</div>
								<ImageCropper.Controls class="justify-end">
									<ImageCropper.Cancel disabled={avatarUploading} />
									<ImageCropper.Crop disabled={avatarUploading} />
								</ImageCropper.Controls>
							</ImageCropper.Dialog>

							<div class="flex flex-col items-start justify-between gap-4 sm:flex-row sm:items-center">
								<div class="flex min-w-0 items-center gap-4">
									<ImageCropper.UploadTrigger
										aria-label={m.account_upload_photo()}
										class={cn(
											'group/avatar relative size-16 overflow-hidden rounded-xl focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:outline-none',
											avatarUploading && 'pointer-events-none opacity-70'
										)}
										disabled={avatarUploading}
									>
										{#key avatarCacheBuster}
											<Avatar.Root class="size-16 rounded-xl transition-all group-hover/avatar:opacity-80">
												{#if avatarSrc}
													<Avatar.Image src={avatarSrc} alt={currentUser.displayName ?? currentUser.username} />
												{:else if avatarUrl}
													<Avatar.Image src={avatarUrl} alt={currentUser.displayName ?? currentUser.username} />
												{/if}
												<Avatar.Fallback class="rounded-xl bg-primary text-xl font-semibold text-primary-foreground">
													{(currentUser.displayName ?? currentUser.username).charAt(0).toUpperCase()}
												</Avatar.Fallback>
											</Avatar.Root>
										{/key}
										<div
											class="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover/avatar:opacity-100"
										>
											<div class="text-xs font-medium text-white">{m.upload()}</div>
										</div>
									</ImageCropper.UploadTrigger>
									<div class="flex min-w-0 flex-col items-start gap-1">
										<div class="text-sm font-medium">@{currentUser.username}</div>
										<div class="text-xs text-muted-foreground">
											{isOidcUser ? m.account_single_sign_on() : m.account_local_account()}
										</div>
										{#if currentUser.avatarUrl}
											<div class="mt-1 flex items-center gap-2">
												<ArcaneButton
													action="remove"
													size="sm"
													tone="ghost"
													customLabel={m.common_remove()}
													showLabel={true}
													class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
													onclick={removeAvatar}
													disabled={avatarUploading}
												/>
											</div>
										{/if}
									</div>
								</div>
								<div class="hidden text-right sm:block">
									{#if safeFormatDate(currentUser.createdAt, 'PP')}
										<div class="text-xs text-muted-foreground">
											{m.account_member_since()}
											{safeFormatDate(currentUser.createdAt, 'PP')}
										</div>
									{/if}
									<div class="text-xs text-muted-foreground" title={currentUser.lastLogin ?? ''}>
										{m.account_last_login_prefix()}
										{safeFormatRelative(currentUser.lastLogin) ?? m.common_never()}
									</div>
								</div>
							</div>
						</ImageCropper.Root>

						<div class="grid gap-5 sm:grid-cols-2">
							<TextInputWithLabel
								id="account-display-name"
								bind:value={profileDisplayName}
								label={m.common_display_name()}
								placeholder={m.account_display_name_placeholder()}
								disabled={isOidcUser}
							/>
							<TextInputWithLabel
								id="account-email"
								type="email"
								bind:value={profileEmail}
								label={m.common_email()}
								placeholder={m.account_email_placeholder()}
								disabled={isOidcUser}
							/>
						</div>
						{#if !isOidcUser}
							<div class="flex justify-end gap-2">
								<ArcaneButton
									action="cancel"
									tone="outline"
									customLabel={m.common_reset()}
									onclick={resetProfile}
									disabled={!profileDirty || profileSaving}
								/>
								<ArcaneButton
									action="save"
									customLabel={m.account_save_profile()}
									onclick={saveProfile}
									loading={profileSaving}
									disabled={!profileDirty || profileSaving}
								/>
							</div>
						{:else}
							<p class="text-xs text-muted-foreground">{m.account_profile_managed_by_idp()}</p>
						{/if}
					</div>
				</Card>

				<!-- Password -->
				{#if !isOidcUser}
					<Card class="overflow-hidden">
						<div class="border-b p-4 sm:p-6">
							<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.common_password()}</h2>
							<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_password_desc()}</p>
						</div>
						<div class="space-y-5 p-4 sm:p-6">
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
						</div>
					</Card>
				{/if}

				<!-- API keys -->
				<Card class="overflow-hidden">
					<div class="flex items-start justify-between gap-3 border-b p-4 sm:p-6">
						<div class="min-w-0">
							<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_api_keys_title()}</h2>
							<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_api_keys_description()}</p>
						</div>
						{#if !showCreateKeyForm && !createdKey}
							<ArcaneButton
								action="create"
								tone="outline"
								size="sm"
								customLabel={m.account_new_key()}
								icon={AddIcon}
								onclick={() => (showCreateKeyForm = true)}
							/>
						{/if}
					</div>

					<div class="p-4 sm:p-6">
						{#if createdKey}
							<div class="mb-4 space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-4">
								<div>
									<div class="text-sm font-semibold">Key created: {createdKey.name}</div>
									<p class="mt-1 text-xs text-muted-foreground">Copy this token now &mdash; you won't be able to see it again.</p>
								</div>
								<div class="flex items-center gap-2">
									<code class="flex-1 truncate rounded border bg-background px-3 py-2 font-mono text-xs">
										{createdKey.key}
									</code>
									<ArcaneButton
										action="base"
										tone="outline"
										size="sm"
										customLabel={m.common_copy()}
										icon={CopyIcon}
										onclick={() => copyKeyToClipboard(createdKey!.key)}
									/>
								</div>
								<div class="flex justify-end">
									<ArcaneButton
										action="cancel"
										tone="ghost"
										size="sm"
										customLabel={m.common_ive_saved_it()}
										onclick={() => (createdKey = null)}
									/>
								</div>
							</div>
						{/if}

						{#if apiKeysLoading && apiKeys.length === 0}
							<div class="py-8 text-center text-sm text-muted-foreground">Loading keys…</div>
						{:else if apiKeys.length === 0}
							<div class="py-8 text-center text-sm text-muted-foreground">
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
												<code class="rounded bg-muted/40 px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
													{key.keyPrefix}…
												</code>
											</div>
											{#if key.description}
												<div class="mt-0.5 truncate text-xs text-muted-foreground">{key.description}</div>
											{/if}
											<div class="mt-1 text-xs text-muted-foreground">
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
											customLabel={m.common_delete()}
											showLabel={false}
											class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
											onclick={() => deleteApiKey(key.id, key.name)}
										/>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				</Card>
			</div>

			<!-- Right column: preferences + roles + danger zone -->
			<div class="space-y-6">
				<!-- Preferences -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_preferences()}</h2>
						<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_preferences_desc()}</p>
					</div>
					<div class="divide-y p-2">
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">{m.account_theme()}</div>
								<div class="text-xs text-muted-foreground">{m.appearance_theme_current_user_description()}</div>
							</div>
							<ThemeModeSelector />
						</div>
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">{m.language()}</div>
								<div class="text-xs text-muted-foreground">{m.account_language_desc()}</div>
							</div>
							<LocalePicker inline />
						</div>
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">{m.time_format()}</div>
								<div class="text-xs text-muted-foreground">{m.account_time_format_desc()}</div>
							</div>
							<TimeFormatPicker id="accountTimeFormatPicker" />
						</div>
						<div class="flex items-center justify-between gap-4 p-3">
							<div class="min-w-0">
								<div class="text-sm font-medium">{m.font_size()}</div>
								<div class="text-xs text-muted-foreground">{m.font_size_description()}</div>
							</div>
							<FontSizePicker />
						</div>
					</div>
				</Card>

				<!-- Roles & access -->
				<Card class="overflow-hidden">
					<div class="border-b p-4 sm:p-6">
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_roles_and_access()}</h2>
						<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_roles()}</p>
					</div>
					<div class="p-4 sm:p-6">
						{#if currentUser.roleAssignments && currentUser.roleAssignments.length > 0}
							<ul class="space-y-2">
								{#each currentUser.roleAssignments as ra (`${ra.roleId}-${ra.environmentId ?? 'global'}`)}
									<li class="flex items-center justify-between gap-3 rounded-lg bg-muted/30 px-3 py-2">
										<div class="min-w-0">
											<div class="text-sm font-medium">{prettyRoleName(ra.roleId)}</div>
											<div class="text-xs text-muted-foreground">
												{ra.environmentId ? m.account_role_environment({ env: ra.environmentId }) : m.account_global_scope()}
												{#if ra.source === 'oidc'}
													<span class="ml-1 opacity-70">{m.account_via_sso()}</span>
												{/if}
											</div>
										</div>
									</li>
								{/each}
							</ul>
						{:else}
							<p class="text-sm text-muted-foreground">{m.account_no_roles()}</p>
						{/if}

						{#if currentUser.permissionsByEnv}
							{@const envCount = Object.keys(currentUser.permissionsByEnv).length}
							{@const globalCount = currentUser.permissionsByEnv[GLOBAL_SCOPE]?.length ?? 0}
							<p class="mt-3 text-xs text-muted-foreground">
								{globalCount} global permission{globalCount === 1 ? '' : 's'} across {envCount} environment{envCount === 1
									? ''
									: 's'}.
							</p>
						{/if}
					</div>
				</Card>

				{#if !autoLoginEnabled}
					<Card class="overflow-hidden border-destructive/30">
						<div class="border-b border-destructive/20 p-4 sm:p-6">
							<div class="flex items-center gap-2">
								<ShieldAlertIcon class="size-5 text-destructive" />
								<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_danger_zone()}</h2>
							</div>
							<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_danger_zone_desc()}</p>
						</div>
						<div class="space-y-4 p-4 sm:p-6">
							<div class="space-y-2">
								<div class="text-sm font-medium">{m.account_signout_other()}</div>
								<p class="text-xs text-muted-foreground">
									{m.account_signout_other_desc()}
								</p>
								<ArcaneButton
									action="restart"
									tone="outline"
									customLabel={m.account_signout_other()}
									onclick={logoutAllOther}
									loading={revokingAll}
									disabled={revokingAll}
								/>
							</div>

							<Separator />

							<div class="space-y-2">
								<div class="text-sm font-medium">{m.common_log_out()}</div>
								<p class="text-xs text-muted-foreground">{m.account_signout_this()}</p>
								<form action="/logout" method="POST">
									<ArcaneButton
										action="cancel"
										tone="outline"
										customLabel={m.common_log_out()}
										icon={LogoutIcon}
										type="submit"
										class="border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive"
									/>
								</form>
							</div>
						</div>
					</Card>
				{/if}
			</div>
		</div>
	{:else}
		<div class="py-12 text-center text-sm text-muted-foreground">Loading account…</div>
	{/if}
</div>

<ApiKeyFormSheet
	bind:open={showCreateKeyForm}
	apiKeyToEdit={null}
	mode="personal"
	onSubmit={createApiKey}
	isLoading={creatingKey}
/>

<script lang="ts">
	import { fromStore } from 'svelte/store';
	import { toast } from 'svelte-sonner';
	import HeaderCard from '#lib/components/header-card.svelte';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import * as Avatar from '#lib/components/ui/avatar/index.js';
	import * as ImageCropper from '#lib/components/ui/image-cropper/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { userService } from '#lib/services/user-service.js';
	import userStore from '#lib/stores/user-store.js';
	import settingsStore from '#lib/stores/config-store.js';
	import { getDefaultProfilePicture } from '#lib/utils/docker.js';
	import { avatarUploadLimitBytes, prepareAvatarUploadFile } from '#lib/utils/avatar-upload.js';
	import { formatDate, formatRelativeTime } from '#lib/utils/formatting.js';
	import { cn } from '#lib/utils.js';
	import { GLOBAL_SCOPE } from '#lib/types/auth.js';
	import { Temporal } from 'temporal-polyfill';
	import { UserIcon, SettingsIcon } from '#lib/icons/index.js';
	import AccountPreferencesPanel from './account-preferences-panel.svelte';
	import AccountApiKeysPanel from './account-api-keys-panel.svelte';
	import AccountSecurityPanel from './account-security-panel.svelte';

	type AccountTab = 'account' | 'preferences';

	const accountTabItems = $derived.by(
		() =>
			[
				{ value: 'account', label: m.common_account(), icon: UserIcon },
				{ value: 'preferences', label: m.account_preferences(), icon: SettingsIcon }
			] satisfies TabItem[]
	);
	const urlTab = useUrlTab<AccountTab>({
		validTabs: () => accountTabItems.map((tab) => tab.value as AccountTab),
		defaultTab: () => 'account'
	});
	const activeTab = $derived(urlTab.value);

	const BUILT_IN_ROLE_LABELS: Record<string, () => string> = {
		role_admin: m.account_role_administrator,
		role_editor: m.account_role_editor,
		role_deployer: m.account_role_deployer,
		role_viewer: m.account_role_viewer
	};

	function prettyRoleName(roleId: string): string {
		return BUILT_IN_ROLE_LABELS[roleId]?.() ?? roleId.replace(/^role_/, '').replace(/_/g, ' ');
	}

	const currentUser = $derived($userStore);
	const isOidcUser = $derived(Boolean(currentUser?.oidcSubjectId));

	const settings = fromStore(settingsStore);
	const gravatarEnabled = $derived(Boolean(settings.current?.enableGravatar));
	const avatarMaxUploadSizeMb = $derived(
		Number(settings.current?.avatarMaxUploadSizeMb) > 0 ? Number(settings.current?.avatarMaxUploadSizeMb) : 2
	);
	const avatarMaxUploadSizeBytes = $derived(avatarUploadLimitBytes(avatarMaxUploadSizeMb));

	let profileDisplayName = $state('');
	let profileEmail = $state('');
	let profileSaving = $state(false);
	let profileLoaded = $state(false);

	let avatarUrl = $state<string>(getDefaultProfilePicture());
	let avatarCacheBuster = $state(Temporal.Now.instant().epochMilliseconds);
	const avatarSrc = $derived(currentUser?.avatarUrl ? `${currentUser.avatarUrl}?t=${avatarCacheBuster}` : '');
	let cropperAvatarSrc = $derived(avatarSrc || avatarUrl);

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
			const msg = err instanceof Error ? err.message : m.common_update_failed({ resource: m.account_profile_title() });
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
			avatarCacheBuster = Temporal.Now.instant().epochMilliseconds;
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
			avatarCacheBuster = Temporal.Now.instant().epochMilliseconds;
			toast.success(m.account_avatar_remove_success());
		} catch (err) {
			toast.error(err instanceof Error ? err.message : m.account_avatar_remove_failed());
		} finally {
			avatarUploading = false;
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
		<Tabs.Root value={activeTab}>
			<TabBar items={accountTabItems} value={activeTab} onValueChange={urlTab.select} />

			<Tabs.Content value="account" class="mt-6 space-y-10">
				<section class="space-y-5">
					<div>
						<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.account_profile_title()}</h2>
						<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.account_profile_description()}</p>
					</div>
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
								{#if formatDate(currentUser.createdAt)}
									<div class="text-xs text-muted-foreground">
										{m.account_member_since()}
										{formatDate(currentUser.createdAt)}
									</div>
								{/if}
								<div class="text-xs text-muted-foreground" title={currentUser.lastLogin ?? ''}>
									{m.account_last_login_prefix()}
									{formatRelativeTime(currentUser.lastLogin) || m.common_never()}
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

					<div class="space-y-2 border-t border-border/50 pt-5">
						<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
							{m.account_roles_and_access()}
						</h3>
						{#if currentUser.roleAssignments && currentUser.roleAssignments.length > 0}
							<ul class="flex flex-wrap gap-2">
								{#each currentUser.roleAssignments as ra (`${ra.roleId}-${ra.environmentId ?? 'global'}`)}
									<li class="rounded-lg border border-border/60 bg-muted/20 px-3 py-1.5">
										<span class="text-sm font-medium">{prettyRoleName(ra.roleId)}</span>
										<span class="ml-2 text-xs text-muted-foreground">
											{ra.environmentId ? m.account_role_environment({ env: ra.environmentId }) : m.account_global_scope()}
											{#if ra.source === 'oidc'}
												<span class="ml-1 opacity-70">{m.account_via_sso()}</span>
											{/if}
										</span>
									</li>
								{/each}
							</ul>
						{:else}
							<p class="text-sm text-muted-foreground">{m.account_no_roles()}</p>
						{/if}

						{#if currentUser.permissionsByEnv}
							{@const envCount = Object.keys(currentUser.permissionsByEnv).length}
							{@const globalCount = currentUser.permissionsByEnv[GLOBAL_SCOPE]?.length ?? 0}
							<p class="text-xs text-muted-foreground">
								{m.account_permissions_summary({ globalCount, environmentCount: envCount })}
							</p>
						{/if}
					</div>
				</section>

				<AccountApiKeysPanel />
				<AccountSecurityPanel />
			</Tabs.Content>

			<Tabs.Content value="preferences" class="mt-6">
				<AccountPreferencesPanel />
			</Tabs.Content>
		</Tabs.Root>
	{:else}
		<div class="py-12 text-center text-sm text-muted-foreground">{m.account_loading()}</div>
	{/if}
</div>

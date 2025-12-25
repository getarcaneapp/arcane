<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import * as Alert from '$lib/components/ui/alert';
	import * as InputGroup from '$lib/components/ui/input-group/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Label } from '$lib/components/ui/label';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { authService } from '$lib/services/auth-service';
	import { toast } from 'svelte-sonner';
	import { EyeOnIcon, EyeOffIcon, AlertIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	let {
		open = $bindable(false),
		onSuccess
	}: {
		open?: boolean;
		onSuccess?: () => void;
	} = $props();

	let currentPassword = $state('arcane-admin');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let isLoading = $state(false);
	let error = $state('');
	let showCurrentPassword = $state(false);
	let showNewPassword = $state(false);
	let showConfirmPassword = $state(false);

	const isValid = $derived(currentPassword.length > 0 && newPassword.length >= 8 && confirmPassword === newPassword);

	async function handleSubmit() {
		if (!isValid) {
			if (newPassword.length < 8) {
				error = m.first_login_error_length();
			} else if (confirmPassword !== newPassword) {
				error = m.first_login_error_mismatch();
			}
			return;
		}

		error = '';
		isLoading = true;

		try {
			await authService.changePassword(currentPassword, newPassword);
			toast.success(m.first_login_success());
			open = false;
			onSuccess?.();
		} catch (err: any) {
			error = err.message || m.first_login_error_failed();
		} finally {
			isLoading = false;
		}
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={(isOpen) => {
		if (!isOpen) {
			open = true;
		}
	}}
	title={m.first_login_title()}
	description={m.first_login_description()}
	contentClass="sm:max-w-[425px] [&>button]:hidden"
>
	{#snippet children()}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				handleSubmit();
			}}
			class="space-y-4"
		>
			{#if error}
				<Alert.Root variant="destructive">
					<AlertIcon class="size-4" />
					<Alert.Title>{m.error_generic()}</Alert.Title>
					<Alert.Description>{error}</Alert.Description>
				</Alert.Root>
			{/if}

			<div class="space-y-2">
				<Label for="current-password">{m.first_login_current_password()}</Label>
				<InputGroup.Root>
					<InputGroup.Input
						id="current-password"
						type={showCurrentPassword ? 'text' : 'password'}
						bind:value={currentPassword}
						placeholder={m.first_login_current_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<InputGroup.Addon align="inline-end">
						<InputGroup.Button
							type="button"
							size="icon-xs"
							onclick={() => (showCurrentPassword = !showCurrentPassword)}
							disabled={isLoading}
							aria-label={showCurrentPassword ? 'Hide password' : 'Show password'}
						>
							{#if showCurrentPassword}
								<EyeOffIcon />
							{:else}
								<EyeOnIcon />
							{/if}
						</InputGroup.Button>
					</InputGroup.Addon>
				</InputGroup.Root>
			</div>

			<div class="space-y-2">
				<Label for="new-password">{m.first_login_new_password()}</Label>
				<InputGroup.Root>
					<InputGroup.Input
						id="new-password"
						type={showNewPassword ? 'text' : 'password'}
						bind:value={newPassword}
						placeholder={m.first_login_new_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<InputGroup.Addon align="inline-end">
						<InputGroup.Button
							type="button"
							size="icon-xs"
							onclick={() => (showNewPassword = !showNewPassword)}
							disabled={isLoading}
							aria-label={showNewPassword ? 'Hide password' : 'Show password'}
						>
							{#if showNewPassword}
								<EyeOffIcon />
							{:else}
								<EyeOnIcon />
							{/if}
						</InputGroup.Button>
					</InputGroup.Addon>
				</InputGroup.Root>
			</div>

			<div class="space-y-2">
				<Label for="confirm-password">{m.first_login_confirm_password()}</Label>
				<InputGroup.Root>
					<InputGroup.Input
						id="confirm-password"
						type={showConfirmPassword ? 'text' : 'password'}
						bind:value={confirmPassword}
						placeholder={m.first_login_confirm_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<InputGroup.Addon align="inline-end">
						<InputGroup.Button
							type="button"
							size="icon-xs"
							onclick={() => (showConfirmPassword = !showConfirmPassword)}
							disabled={isLoading}
							aria-label={showConfirmPassword ? 'Hide password' : 'Show password'}
						>
							{#if showConfirmPassword}
								<EyeOffIcon />
							{:else}
								<EyeOnIcon />
							{/if}
						</InputGroup.Button>
					</InputGroup.Addon>
				</InputGroup.Root>
			</div>
		</form>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton
			type="submit"
			onclick={handleSubmit}
			disabled={!isValid || isLoading}
			loading={isLoading}
			action="confirm"
			customLabel={m.first_login_submit()}
			loadingLabel={m.first_login_submitting()}
		/>
	{/snippet}
</ResponsiveDialog>

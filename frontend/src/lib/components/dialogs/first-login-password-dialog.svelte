<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { authService } from '$lib/services/auth-service';
	import { toast } from 'svelte-sonner';
	import CircleAlert from 'phosphor-svelte/lib/Warning';
	import Eye from 'phosphor-svelte/lib/Eye';
	import EyeOff from 'phosphor-svelte/lib/EyeClosed';
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
					<CircleAlert class="h-4 w-4" />
					<Alert.Title>Error</Alert.Title>
					<Alert.Description>{error}</Alert.Description>
				</Alert.Root>
			{/if}

			<div class="space-y-2">
				<Label for="current-password">{m.first_login_current_password()}</Label>
				<div class="relative">
					<Input
						id="current-password"
						type={showCurrentPassword ? 'text' : 'password'}
						bind:value={currentPassword}
						placeholder={m.first_login_current_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<Button
						type="button"
						variant="ghost"
						size="sm"
						class="absolute top-0 right-0 h-full px-3 py-2 hover:bg-transparent"
						onclick={() => (showCurrentPassword = !showCurrentPassword)}
						disabled={isLoading}
					>
						{#if showCurrentPassword}
							<EyeOff class="h-4 w-4" />
						{:else}
							<Eye class="h-4 w-4" />
						{/if}
					</Button>
				</div>
			</div>

			<div class="space-y-2">
				<Label for="new-password">{m.first_login_new_password()}</Label>
				<div class="relative">
					<Input
						id="new-password"
						type={showNewPassword ? 'text' : 'password'}
						bind:value={newPassword}
						placeholder={m.first_login_new_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<Button
						type="button"
						variant="ghost"
						size="sm"
						class="absolute top-0 right-0 h-full px-3 py-2 hover:bg-transparent"
						onclick={() => (showNewPassword = !showNewPassword)}
						disabled={isLoading}
					>
						{#if showNewPassword}
							<EyeOff class="h-4 w-4" />
						{:else}
							<Eye class="h-4 w-4" />
						{/if}
					</Button>
				</div>
			</div>

			<div class="space-y-2">
				<Label for="confirm-password">{m.first_login_confirm_password()}</Label>
				<div class="relative">
					<Input
						id="confirm-password"
						type={showConfirmPassword ? 'text' : 'password'}
						bind:value={confirmPassword}
						placeholder={m.first_login_confirm_password_placeholder()}
						required
						disabled={isLoading}
					/>
					<Button
						type="button"
						variant="ghost"
						size="sm"
						class="absolute top-0 right-0 h-full px-3 py-2 hover:bg-transparent"
						onclick={() => (showConfirmPassword = !showConfirmPassword)}
						disabled={isLoading}
					>
						{#if showConfirmPassword}
							<EyeOff class="h-4 w-4" />
						{:else}
							<Eye class="h-4 w-4" />
						{/if}
					</Button>
				</div>
			</div>
		</form>
	{/snippet}

	{#snippet footer()}
		<Button type="submit" onclick={handleSubmit} disabled={!isValid || isLoading}>
			{#if isLoading}
				<Spinner class="mr-2 h-4 w-4" />
				{m.first_login_submitting()}
			{:else}
				{m.first_login_submit()}
			{/if}
		</Button>
	{/snippet}
</ResponsiveDialog>

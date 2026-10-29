<script lang="ts">
	import { createMutation } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { EyeOnIcon, EyeOffIcon, AlertIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { authService } from '#lib/services/auth-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

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
	let error = $state('');
	let showCurrentPassword = $state(false);
	let showNewPassword = $state(false);
	let showConfirmPassword = $state(false);

	const passwordRequirements = $derived.by(() => {
		switch (settingsStore.current?.authPasswordPolicy) {
			case 'basic':
				return m.security_password_policy_basic_tooltip();
			case 'standard':
				return m.security_password_policy_standard_tooltip();
			default:
				return m.security_password_policy_strong_tooltip();
		}
	});
	const formSchema = z
		.object({
			currentPassword: z.string().min(1, m.common_required()),
			newPassword: z.string().min(1, m.common_required()),
			confirmPassword: z.string()
		})
		.refine((data) => data.newPassword === data.confirmPassword, {
			message: m.first_login_error_mismatch(),
			path: ['confirmPassword']
		});
	const validation = $derived(formSchema.safeParse({ currentPassword, newPassword, confirmPassword }));
	const changePasswordMutation = createMutation(() => ({
		mutationFn: ({ currentPassword, newPassword }: { currentPassword: string; newPassword: string }) =>
			authService.changePassword(currentPassword, newPassword),
		onSuccess: () => {
			toast.success(m.first_login_success());
			open = false;
			onSuccess?.();
		},
		onError: (err) => {
			error = extractApiErrorMessage(err);
		}
	}));

	const isLoading = $derived(changePasswordMutation.isPending);

	function handleSubmit() {
		if (!validation.success) {
			error = validation.error.issues[0]?.message ?? m.first_login_error_failed();
			return;
		}

		error = '';
		changePasswordMutation.mutate(validation.data);
	}
</script>

<ResponsiveDialog
	{open}
	dismissible={false}
	title={m.first_login_title()}
	description={m.first_login_description()}
	contentClass="sm:max-w-106.25"
>
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
					placeholder={m.users_password_enter()}
					aria-describedby="new-password-requirements"
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

		<p id="new-password-requirements" class="text-sm text-muted-foreground">{passwordRequirements}</p>

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

	{#snippet footer()}
		<ArcaneButton
			type="submit"
			onclick={handleSubmit}
			disabled={!validation.success || isLoading}
			loading={isLoading}
			action="confirm"
			customLabel={m.first_login_submit()}
			loadingLabel={m.first_login_submitting()}
		/>
	{/snippet}
</ResponsiveDialog>

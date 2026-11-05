<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import LabeledSwitch from '#lib/components/form/labeled-switch.svelte';
	import SheetFooterActions from '#lib/components/sheets/sheet-footer-actions.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { UseClipboard } from '#lib/hooks/use-clipboard.svelte.js';
	import { AddIcon, AlertTriangleIcon, ApiKeyIcon, CopyIcon, TrashIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { enrollmentTokensQuery } from '#lib/query/environments.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import type { EnrollmentToken, EnrollmentTokenCreate, EnrollmentTokenCreated } from '#lib/types/environment.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDate, formatRelativeTime } from '#lib/utils/formatting.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	const queryClient = useQueryClient();
	let showCreateForm = $state(false);
	let createdToken = $state<EnrollmentTokenCreated | null>(null);
	let deletingTokenId = $state<string | null>(null);
	let copyingTokenId = $state<string | null>(null);
	const clipboard = new UseClipboard();

	const tokensQuery = createQuery(() => enrollmentTokensQuery());

	const tokenForm = createForm(
		z
			.object({
				name: z
					.string()
					.min(1, m.common_name_required())
					.max(100, m.common_max_length({ field: m.common_name(), maxLength: 100 })),
				unlimited: z.boolean(),
				maxUses: z.number().optional().catch(undefined)
			})
			.superRefine((data, ctx) => {
				const maxUses = data.maxUses ?? 0;
				if (data.unlimited || (Number.isInteger(maxUses) && maxUses >= 1 && maxUses <= 10000)) return;
				ctx.addIssue({ code: 'custom', path: ['maxUses'], message: m.max_uses_invalid() });
			}),
		{ name: '', unlimited: true, maxUses: undefined }
	);
	const tokenInputs = $derived(tokenForm.inputs);

	const createTokenMutation = createMutation(() => ({
		mutationFn: (request: EnrollmentTokenCreate) => environmentManagementService.createEnrollmentToken(request),
		onSuccess: async (created) => {
			createdToken = created;
			showCreateForm = false;
			tokenForm.reset();
			toast.success(m.common_create_success({ resource: m.enrollment_token() }));
			await queryClient.invalidateQueries({ queryKey: queryKeys.environments.enrollmentTokens });
		},
		onError: (error) => {
			toast.error(m.common_create_failed({ resource: m.enrollment_token() }), { description: extractApiErrorMessage(error) });
		}
	}));

	function handleCreate() {
		const data = tokenForm.validate();
		if (!data) return;
		createTokenMutation.mutate({ name: data.name, maxUses: data.unlimited ? undefined : data.maxUses });
	}

	function handleOpenChange(isOpen: boolean) {
		showCreateForm = isOpen;
		if (!isOpen) tokenForm.reset();
	}

	async function copyToken(token: EnrollmentToken) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(environmentManagementService.getEnrollmentTokenSecret(token.id)),
			message: m.common_copy_failed(),
			setLoadingState: (loading) => (copyingTokenId = loading ? token.id : null),
			onSuccess: async (secret) => {
				if ((await clipboard.copy(secret.token)) === 'success') {
					toast.success(m.common_copied());
					return;
				}
				toast.error(m.common_copy_failed());
			}
		});
	}

	function deleteToken(token: EnrollmentToken) {
		openConfirmDialog({
			title: m.common_delete_title({ resource: token.name }),
			message: m.common_delete_confirm({ resource: token.name }),
			confirm: {
				label: m.common_delete(),
				destructive: true,
				action: async () => {
					await handleApiResultWithCallbacks({
						result: await tryCatch(environmentManagementService.deleteEnrollmentToken(token.id)),
						message: m.common_delete_failed({ resource: token.name }),
						setLoadingState: (loading) => (deletingTokenId = loading ? token.id : null),
						onSuccess: async () => {
							toast.success(m.common_delete_success({ resource: token.name }));
							await queryClient.invalidateQueries({ queryKey: queryKeys.environments.enrollmentTokens });
						}
					});
				}
			}
		});
	}
</script>

<section class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<h2 class="text-base font-semibold tracking-tight sm:text-lg">{m.enrollment_tokens()}</h2>
			<p class="mt-1 text-xs text-muted-foreground sm:text-sm">{m.enrollment_tokens_description()}</p>
		</div>
		{#if !createdToken}
			<ArcaneButton
				action="create"
				customLabel={m.common_create_button({ resource: m.enrollment_token() })}
				icon={AddIcon}
				class="shrink-0"
				onclick={() => (showCreateForm = true)}
			/>
		{/if}
	</div>

	{#if createdToken}
		<div class="space-y-4 rounded-lg border border-primary/30 bg-primary/5 p-4">
			<div class="space-y-2">
				<div class="truncate text-sm font-semibold">{m.enrollment_token()}: {createdToken.name}</div>
				<div class="flex items-center gap-2">
					<code class="flex-1 rounded-md border bg-background px-3 py-2 font-mono text-sm break-all">{createdToken.token}</code>
					<CopyButton text={createdToken.token} size="icon" class="size-7" />
				</div>
			</div>

			<div class="flex justify-end">
				<ArcaneButton action="base" tone="outline" customLabel={m.common_done()} onclick={() => (createdToken = null)} />
			</div>
		</div>
	{/if}

	{#if tokensQuery.isError}
		<Alert.Root variant="destructive">
			<AlertTriangleIcon class="size-4" />
			<Alert.Title>{m.common_load_failed({ resource: m.enrollment_tokens() })}</Alert.Title>
			<Alert.Description>{extractApiErrorMessage(tokensQuery.error)}</Alert.Description>
		</Alert.Root>
	{:else}
		<div class="overflow-hidden rounded-xl border bg-background/60">
			{#if tokensQuery.isPending}
				<div class="flex items-center justify-center py-8">
					<Spinner class="size-6" />
				</div>
			{:else if tokensQuery.data.length === 0}
				<div class="p-6 text-center text-sm text-muted-foreground">
					<ApiKeyIcon class="mx-auto mb-2 size-8 opacity-40" />
					{m.enrollment_tokens_empty()}
				</div>
			{:else}
				<ul class="divide-y divide-border/40">
					{#each tokensQuery.data as token (token.id)}
						<li class="flex items-start justify-between gap-2 px-6 py-3">
							<div class="min-w-0 flex-1">
								<div class="truncate text-sm font-medium">{token.name}</div>
								<div class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
									<span>{m.uses()} {token.useCount} / {token.maxUses ?? m.unlimited()}</span>
									<span aria-hidden="true">·</span>
									{#if token.environmentName}
										<span>{m.resource_environment_cap()} {token.environmentName}</span>
										<span aria-hidden="true">·</span>
									{/if}
									<span>{m.common_created()} {formatDate(token.createdAt)}</span>
									<span aria-hidden="true">·</span>
									<span>{m.last_used()} {formatRelativeTime(token.lastUsedAt) || m.common_never()}</span>
								</div>
							</div>
							<div class="flex shrink-0 items-center gap-1">
								{#if !token.environmentId || hasPermission('environments:pair', token.environmentId)}
									<ArcaneButton
										action="base"
										tone="ghost"
										size="icon"
										icon={CopyIcon}
										customLabel={m.common_copy()}
										showLabel={false}
										loading={copyingTokenId === token.id}
										disabled={copyingTokenId !== null}
										class="text-muted-foreground"
										onclick={() => copyToken(token)}
									/>
								{/if}
								<ArcaneButton
									action="remove"
									tone="ghost"
									size="icon"
									icon={TrashIcon}
									customLabel={m.common_delete()}
									showLabel={false}
									loading={deletingTokenId === token.id}
									disabled={deletingTokenId !== null}
									class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
									onclick={() => deleteToken(token)}
								/>
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}
</section>

<ResponsiveDialog.Root
	open={showCreateForm}
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={m.common_create_button({ resource: m.enrollment_token() })}
	description={m.enrollment_tokens_description()}
	contentClass="sm:max-w-125"
>
	<form onsubmit={preventDefault(handleCreate)} class="grid gap-4 py-6">
		<FormInput label={m.common_name()} placeholder={m.enrollment_token_name_placeholder()} bind:input={tokenInputs.name} />
		<LabeledSwitch id="enrollment-token-unlimited" label={m.unlimited()} bind:checked={tokenInputs.unlimited.value} />
		{#if !tokenInputs.unlimited.value}
			<FormInput label={m.max_uses()} type="number" placeholder="10" bind:input={tokenInputs.maxUses} />
		{/if}
	</form>

	{#snippet footer()}
		<SheetFooterActions
			bind:open={showCreateForm}
			onCancel={() => handleOpenChange(false)}
			cancelDisabled={createTokenMutation.isPending}
			submitDisabled={createTokenMutation.isPending}
			submitLoading={createTokenMutation.isPending}
			onSubmit={handleCreate}
			submitLabel={m.common_create()}
		/>
	{/snippet}
</ResponsiveDialog.Root>

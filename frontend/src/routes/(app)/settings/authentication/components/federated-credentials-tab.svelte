<script lang="ts">
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import FederatedCredentialFormSheet from '#lib/components/sheets/federated-credential-form-sheet.svelte';
	import * as ResponsiveDialog from '#lib/components/ui/responsive-dialog/index.js';
	import { Snippet } from '#lib/components/ui/snippet/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { federatedCredentialService } from '#lib/services/federated-credential-service.js';
	import type { FederatedCredential, FederatedCredentialCreate, FederatedCredentialUpdate, Role } from '#lib/types/auth.js';
	import type { Environment } from '#lib/types/environment.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { base } from '#lib/utils/base-path.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import FederatedCredentialTable from './federated-credential-table.svelte';

	interface Props {
		initialFederatedCredentials: Paginated<FederatedCredential>;
		initialRequestOptions: SearchPaginationSortRequest;
		roles: Role[];
		environments: Environment[];
	}

	let { initialFederatedCredentials, initialRequestOptions, roles, environments }: Props = $props();

	let federatedCredentials = $state(untrack(() => initialFederatedCredentials));
	let selectedIds: string[] = $state([]);
	let requestOptions: SearchPaginationSortRequest = $state(untrack(() => initialRequestOptions));

	let isDialogOpen = $state({
		create: false,
		edit: false,
		instructions: false
	});

	let credentialToEdit: FederatedCredential | null = $state(null);
	let newlyCreatedCredential: FederatedCredential | null = $state(null);

	let isLoading = $state({
		creating: false,
		editing: false
	});

	function openCreateDialog() {
		credentialToEdit = null;
		isDialogOpen.create = true;
	}

	function openEditDialog(credential: FederatedCredential) {
		credentialToEdit = credential;
		isDialogOpen.edit = true;
	}

	async function refreshFederatedCredentials() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(federatedCredentialService.list(requestOptions)),
			message: m.common_refresh_failed({ resource: m.federated_credential_page_title() }),
			onSuccess: (list) => {
				federatedCredentials = list;
			}
		});
	}

	async function handleFederatedCredentialSubmit(
		submission:
			| { isEditMode: true; credentialId: string; credential: FederatedCredentialUpdate }
			| { isEditMode: false; credential: FederatedCredentialCreate }
	) {
		const loading = submission.isEditMode ? 'editing' : 'creating';
		isLoading[loading] = true;

		const safeName = submission.credential.name?.trim() || credentialToEdit?.name || m.common_unknown();
		if (submission.isEditMode) {
			const result = await tryCatch(federatedCredentialService.update(submission.credentialId, submission.credential));
			await handleApiResultWithCallbacks({
				result,
				message: m.federated_credential_update_failed({ name: safeName }),
				setLoadingState: (value) => (isLoading[loading] = value),
				onSuccess: async () => {
					toast.success(m.federated_credential_updated_success({ name: safeName }));
					await refreshFederatedCredentials();
					isDialogOpen.edit = false;
					credentialToEdit = null;
				}
			});
		} else {
			const result = await tryCatch(federatedCredentialService.create(submission.credential));
			await handleApiResultWithCallbacks({
				result,
				message: m.federated_credential_create_failed({ name: safeName }),
				setLoadingState: (value) => (isLoading[loading] = value),
				onSuccess: async (createdCredential) => {
					toast.success(m.federated_credential_created_success({ name: safeName }));
					await refreshFederatedCredentials();
					isDialogOpen.create = false;
					newlyCreatedCredential = createdCredential as FederatedCredential;
					isDialogOpen.instructions = true;
				}
			});
		}
	}

	function instructionsSnippet(credential: FederatedCredential | null): string {
		const audience = credential?.audiences?.[0] ?? 'https://arcane.example.com';
		const serverURL = globalThis.location ? `${globalThis.location.origin}${base}` : 'https://arcane.example.com';
		return `permissions: { id-token: write, contents: read }
steps:
  - run: |
      eval "$(arcane-cli auth federated \\
        --server ${serverURL} \\
        --audience ${audience} --export)"
  - run: arcane-cli projects up my-app`;
	}
</script>

<IfPermitted perm="federated:list">
	<div class="space-y-4">
		<div class="flex items-start justify-between gap-4">
			<div class="px-1">
				<h3 class="text-sm font-semibold">{m.federated_credential_page_title()}</h3>
				<p class="text-xs text-muted-foreground">{m.federated_credential_page_description()}</p>
			</div>
			<IfPermitted adminOnly>
				<ArcaneButton
					action="create"
					size="sm"
					onclick={openCreateDialog}
					loading={isLoading.creating}
					disabled={isLoading.creating}
					customLabel={m.create_federated_credential()}
				/>
			</IfPermitted>
		</div>

		<FederatedCredentialTable
			bind:federatedCredentials
			bind:selectedIds
			bind:requestOptions
			onFederatedCredentialsChanged={refreshFederatedCredentials}
			onEditFederatedCredential={openEditDialog}
		/>
	</div>

	<FederatedCredentialFormSheet
		bind:open={isDialogOpen.create}
		credentialToEdit={null}
		{roles}
		{environments}
		onSubmit={handleFederatedCredentialSubmit}
		isLoading={isLoading.creating}
	/>

	<FederatedCredentialFormSheet
		bind:open={isDialogOpen.edit}
		{credentialToEdit}
		{roles}
		{environments}
		onSubmit={handleFederatedCredentialSubmit}
		isLoading={isLoading.editing}
	/>

	<ResponsiveDialog.Root
		bind:open={isDialogOpen.instructions}
		title={m.federated_credential_instructions_title()}
		description={m.federated_credential_instructions_description()}
		contentClass="!max-w-2xl"
	>
		<div class="space-y-4 py-4">
			<div class="rounded-lg bg-muted p-4">
				<p class="mb-2 text-sm font-medium text-muted-foreground">
					{m.federated_credential_instructions_snippet_label()}
				</p>
				<Snippet
					text={instructionsSnippet(newlyCreatedCredential)}
					onCopy={(status) => {
						if (status === 'success') {
							toast.success(m.common_copied());
						}
					}}
				/>
			</div>
		</div>
		{#snippet footer()}
			<ArcaneButton action="confirm" onclick={() => (isDialogOpen.instructions = false)} customLabel={m.common_done()} />
		{/snippet}
	</ResponsiveDialog.Root>

	{#snippet fallback()}
		<div class="rounded-lg border border-dashed border-border/40 bg-muted/20 p-6 text-center">
			<p class="text-sm text-muted-foreground">{m.no_access_page_body()}</p>
		</div>
	{/snippet}
</IfPermitted>

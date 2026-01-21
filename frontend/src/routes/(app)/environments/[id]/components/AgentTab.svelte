<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { m } from '$lib/paraglide/messages';
	import { ApiKeyIcon, ResetIcon } from '$lib/icons';

	let { regeneratedApiKey = $bindable(), isRegeneratingKey, showRegenerateDialog = $bindable() } = $props();
</script>

<Card.Root class="flex flex-col">
	<Card.Header icon={ApiKeyIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.environments_agent_config_title()}</h2>
			</Card.Title>
			<Card.Description>{m.environments_agent_config_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="space-y-4 p-4">
		{#if regeneratedApiKey}
			<div class="space-y-4">
				<div class="space-y-2">
					<div class="text-sm font-medium">{m.environments_new_api_key()}</div>
					<div class="flex items-center gap-2">
						<code class="bg-muted flex-1 rounded-md px-3 py-2 font-mono text-sm break-all">
							{regeneratedApiKey}
						</code>
						<CopyButton text={regeneratedApiKey} size="icon" class="size-7" />
					</div>
					<p class="text-muted-foreground text-xs">{m.environments_api_key_save_warning()}</p>
				</div>
				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => (regeneratedApiKey = null)}
					customLabel={m.common_dismiss()}
					class="w-full"
				/>
			</div>
		{:else}
			<div class="rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-900 dark:text-amber-200">
				<p class="font-medium">{m.environments_regenerate_warning_title()}</p>
				<p class="mt-1">{m.environments_regenerate_warning_message()}</p>
			</div>
			<ArcaneButton
				action="remove"
				onclick={() => {
					showRegenerateDialog = true;
				}}
				disabled={isRegeneratingKey}
				loading={isRegeneratingKey}
				icon={ResetIcon}
				customLabel={m.environments_regenerate_api_key()}
				class="w-full"
			/>
		{/if}
	</Card.Content>
</Card.Root>

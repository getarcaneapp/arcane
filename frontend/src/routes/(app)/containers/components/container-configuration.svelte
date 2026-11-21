<script lang="ts">
	import EnvVarsList from '#lib/components/env-vars-list.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { SettingsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetails } from '#lib/types/container.js';

	interface Props {
		container: ContainerDetails;
	}

	let { container }: Props = $props();
</script>

<Card.Root>
	<Card.Header icon={SettingsIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>
					{m.common_environment_variables()}
				</h2>
			</Card.Title>
			<Card.Description>{m.containers_env_vars_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content>
		{#if container.config?.env && container.config.env.length > 0}
			<EnvVarsList
				envVars={container.config.env}
				nameOnlyLabel={m.swarm_service_env_var()}
				valueTitle={m.common_click_to_select()}
			/>
		{:else}
			<div class="rounded-lg border border-dashed py-8 text-center text-muted-foreground">
				<div class="text-sm">{m.containers_no_env_vars()}</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>

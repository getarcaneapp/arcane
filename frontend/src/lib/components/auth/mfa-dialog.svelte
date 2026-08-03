<script lang="ts">
	import { m } from '#lib/paraglide/messages';
	import * as Dialog from '#lib/components/ui/dialog';
	import { Button } from '#lib/components/ui/button';
	import { ShieldCheckIcon } from '#lib/icons';

	let {
		open = $bindable(false),
		enabled,
		passkeyCount,
		recoveryCodesRemaining,
		onEnable,
		onDisable,
		onRegenerate
	}: {
		open: boolean;
		enabled: boolean;
		passkeyCount: number;
		recoveryCodesRemaining: number;
		onEnable: () => void;
		onDisable: () => void;
		onRegenerate: () => void;
	} = $props();

	// Every action funnels through step-up, which opens its own dialog: close this
	// one first so the two never stack.
	function run(action: () => void) {
		open = false;
		action();
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<ShieldCheckIcon class="size-5 text-primary" />
				{enabled ? m.account_passkey_mfa_disable_title() : m.account_passkey_mfa_title()}
			</Dialog.Title>
			<Dialog.Description>
				{enabled ? m.account_passkey_mfa_disable_description() : m.account_passkey_mfa_description()}
			</Dialog.Description>
		</Dialog.Header>

		<div class="space-y-3 text-sm text-muted-foreground">
			{#if enabled}
				<p>{m.account_passkey_mfa_recovery_codes_remaining({ count: recoveryCodesRemaining })}</p>
				<Button type="button" variant="outline" size="sm" onclick={() => run(onRegenerate)}>
					{m.account_passkey_mfa_regenerate()}
				</Button>
			{:else if passkeyCount === 0}
				<p>{m.account_passkey_mfa_requires_passkey()}</p>
			{:else}
				<p>{m.account_passkey_mfa_ready()}</p>
			{/if}
		</div>

		<Dialog.Footer>
			<Button type="button" variant="ghost" onclick={() => (open = false)}>{m.common_cancel()}</Button>
			{#if enabled}
				<Button type="button" variant="destructive" onclick={() => run(onDisable)}>
					{m.account_passkey_mfa_disable()}
				</Button>
			{:else}
				<Button type="button" disabled={passkeyCount === 0} onclick={() => run(onEnable)}>
					{m.account_passkey_mfa_enable()}
				</Button>
			{/if}
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

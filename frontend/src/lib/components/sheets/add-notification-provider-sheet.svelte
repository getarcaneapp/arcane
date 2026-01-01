<script lang="ts">
	import * as ResponsiveDialog from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import type { NotificationSettings } from '$lib/types/notification.type';
	import { notificationProviders } from '$lib/types/notification.type';
	import { providerSchemas, type ProviderField } from '$lib/constants/notification-providers';
	import { m } from '$lib/paraglide/messages';
	import { AddIcon, SaveIcon } from '$lib/icons';
	import { preventDefault } from '$lib/utils/form.utils';

	type ProviderFormProps = {
		open: boolean;
		providerToEdit: NotificationSettings | null;
		onSubmit: (data: NotificationSettings) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		providerToEdit = $bindable(),
		onSubmit,
		isLoading
	}: ProviderFormProps = $props();

	let isEditMode = $derived(!!providerToEdit);
	let SubmitIcon = $derived(isEditMode ? SaveIcon : AddIcon);

	let name = $state('');
	let provider = $state('discord');
	let enabled = $state(true);
	let config = $state<Record<string, any>>({});

	// Reset form when opening/closing or changing providerToEdit
	$effect(() => {
		if (open && providerToEdit) {
			name = providerToEdit.name;
			provider = providerToEdit.provider;
			enabled = providerToEdit.enabled;
			// Load config from providerToEdit, ensuring we have all fields
			config = { ...providerToEdit.config };
		} else if (open && !providerToEdit) {
			// New provider defaults
			if (!name) name = '';
			if (!provider) provider = 'discord';
			enabled = true;
			config = {};
		}
	});

	let currentSchema = $derived(providerSchemas[provider] || providerSchemas['generic']);

	function constructUrl(p: string, c: Record<string, any>): string {
		switch (p) {
			case 'discord':
				if (c.webhookUrl) {
					const match = c.webhookUrl.match(/webhooks\/(\d+)\/(.+)/);
					if (match) {
						return `discord://${match[2]}@${match[1]}`;
					}
					return c.webhookUrl;
				}
				return '';
			case 'telegram': {
				const query = new URLSearchParams();
				if (c.chatId) query.set('chats', c.chatId);
				if (c.sendSilently) query.set('notification', 'no');
				return `telegram://${c.botToken}@telegram?${query.toString()}`;
			}
			case 'slack': {
				if (c.webhookUrl) {
					const match = c.webhookUrl.match(/services\/(.+)\/(.+)\/(.+)/);
					if (match) {
						return `slack://${match[1]}/${match[2]}/${match[3]}`;
					}
					return c.webhookUrl;
				}
				return '';
			}
			case 'gotify': {
				const host = c.url?.replace(/^https?:\/\//, '') || '';
				const query = new URLSearchParams();
				if (c.priority) query.set('priority', c.priority);
				return `gotify://${host}/${c.token}?${query.toString()}`;
			}
			case 'ntfy': {
				const host = c.url?.replace(/^https?:\/\//, '') || '';
				const query = new URLSearchParams();
				if (c.priority) query.set('priority', c.priority);
				const userPass = c.username && c.password ? `${c.username}:${c.password}@` : '';
				return `ntfy://${userPass}${host}/${c.topic}?${query.toString()}`;
			}
			case 'pushbullet':
				return `pushbullet://${c.accessToken}/${c.channelTag || ''}`;
			case 'pushover': {
				const query = new URLSearchParams();
				if (c.priority) query.set('priority', c.priority);
				if (c.sound) query.set('sound', c.sound);
				return `pushover://shoutrrr:${c.token}@${c.userKey}?${query.toString()}`;
			}
			case 'email': {
				const userPass = c.smtpUsername && c.smtpPassword ? `${c.smtpUsername}:${c.smtpPassword}@` : '';
				const hostPort = `${c.smtpHost}:${c.smtpPort}`;
				const query = new URLSearchParams();
				if (c.fromAddress) query.set('from', c.fromAddress);
				if (c.toAddresses) query.set('to', c.toAddresses);
				// Shoutrrr email format: smtp://user:pass@host:port/?from=...&to=...
				return `smtp://${userPass}${hostPort}/?${query.toString()}`;
			}
			case 'webhook':
				return c.webhookUrl || '';
			default:
				return c.url || '';
		}
	}

	function handleSubmit() {
		if (!name) return; // Basic validation

		const url = constructUrl(provider, config);
		
		// We store the granular config AND the constructed URL
		const finalConfig = {
			...config,
			url
		};

		const providerData: NotificationSettings = {
			id: providerToEdit?.id || '',
			name,
			provider,
			enabled,
			config: finalConfig
		};

		onSubmit(providerData);
	}

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
		if (!newOpenState) {
			providerToEdit = null;
			// Reset state
			name = '';
			provider = 'discord';
			enabled = true;
			config = {};
		}
	}

	const providerOptions = $derived(
		Object.entries(notificationProviders).map(([value, labelFn]) => ({
			value,
			label: labelFn()
		}))
	);
</script>

<ResponsiveDialog.Root
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title={isEditMode ? m.common_edit() : m.common_add_button({ resource: 'Provider' })}
	description={isEditMode ? m.common_edit() : m.common_add_button({ resource: 'Provider' })}
	contentClass="sm:max-w-[500px]"
>
	{#snippet children()}
		<form onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-6">
			<div class="grid gap-2">
				<Label for="name">{m.common_name()}</Label>
				<Input
					id="name"
					type="text"
					placeholder={m.common_name()}
					bind:value={name}
					required
				/>
			</div>

			<SelectWithLabel
				id="provider-select"
				label={m.common_type()}
				bind:value={provider}
				options={providerOptions}
			/>

			<div class="space-y-4 rounded-lg border p-4">
				<h4 class="text-sm font-medium text-muted-foreground">Configuration</h4>
				
				{#each currentSchema as field}
					{#if field.type === 'select'}
						<SelectWithLabel
							id={field.name}
							label={field.label()}
							bind:value={config[field.name]}
							options={field.options || []}
						/>
					{:else if field.type === 'switch'}
						<SwitchWithLabel
							id={field.name}
							label={field.label()}
							bind:checked={config[field.name]}
						/>
					{:else}
						<div class="grid gap-2">
							<Label for={field.name}>{field.label()}</Label>
							<Input
								id={field.name}
								type={field.type}
								placeholder={field.placeholder?.()}
								bind:value={config[field.name]}
								required={field.required}
							/>
							{#if field.description}
								<p class="text-muted-foreground text-[0.8rem]">{field.description()}</p>
							{/if}
						</div>
					{/if}
				{/each}
			</div>

			<SwitchWithLabel
				id="enabledSwitch"
				label={m.common_enabled()}
				bind:checked={enabled}
			/>
		</form>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-row gap-2">
			<ArcaneButton
				action="cancel"
				tone="outline"
				type="button"
				class="flex-1"
				onclick={() => (open = false)}
				disabled={isLoading}
			/>
			<ArcaneButton
				action={isEditMode ? 'save' : 'create'}
				type="submit"
				class="flex-1"
				disabled={isLoading}
				loading={isLoading}
				onclick={handleSubmit}
				customLabel={isEditMode ? m.common_save() : m.common_add_button({ resource: 'Provider' })}
			/>
		</div>
	{/snippet}
</ResponsiveDialog.Root>

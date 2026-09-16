<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import TextInputWithLabel from '#lib/components/form/text-input-with-label.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { AddIcon, CloseIcon } from '#lib/icons/index.js';
	import type { TelegramDestination } from '#lib/types/notifications.js';

	let {
		destinations = $bindable([]),
		errors = {},
		disabled = false
	}: {
		destinations: TelegramDestination[];
		errors?: Record<string, string | undefined>;
		disabled?: boolean;
	} = $props();

	function addDestination() {
		destinations.push({ chatId: '', topicId: '' });
	}

	function removeDestination(index: number) {
		destinations.splice(index, 1);
	}
</script>

<div class="space-y-3">
	<div>
		<Label class="text-sm font-medium">{m.notifications_telegram_destinations_label()}</Label>
		<p class="text-sm text-muted-foreground">{m.notifications_telegram_destinations_help()}</p>
	</div>
	{#each destinations as destination, index (destination)}
		<div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:gap-3">
			<div class="flex-1">
				<TextInputWithLabel
					id={`telegram-chat-id-${index}`}
					value={destination.chatId}
					onChange={(value) => (destination.chatId = value)}
					label={m.notifications_telegram_chat_id_label()}
					placeholder={m.notifications_telegram_chat_id_placeholder()}
					error={errors[`destinations.${index}.chatId`]}
					{disabled}
				/>
			</div>
			<div class="flex-1">
				<TextInputWithLabel
					id={`telegram-topic-id-${index}`}
					value={destination.topicId}
					onChange={(value) => (destination.topicId = value)}
					label={m.notifications_telegram_topic_id_label()}
					placeholder={m.notifications_telegram_topic_id_placeholder()}
					error={errors[`destinations.${index}.topicId`]}
					{disabled}
				/>
			</div>
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				onclick={() => removeDestination(index)}
				disabled={disabled || destinations.length <= 1}
				aria-label={m.common_remove()}
				class="shrink-0 self-end text-destructive hover:text-destructive sm:mt-7 sm:self-auto"
				icon={CloseIcon}
			/>
		</div>
	{/each}
	{#if errors['destinations']}
		<p class="text-sm text-destructive">{errors['destinations']}</p>
	{/if}
	<ArcaneButton
		action="base"
		tone="outline"
		size="sm"
		onclick={addDestination}
		{disabled}
		class="w-fit"
		icon={AddIcon}
		customLabel={m.notifications_telegram_add_destination()}
	/>
</div>

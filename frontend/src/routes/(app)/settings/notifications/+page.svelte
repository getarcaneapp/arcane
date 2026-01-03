<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import * as Tabs from '$lib/components/ui/tabs';
	import * as Dialog from '$lib/components/ui/dialog';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { toast } from 'svelte-sonner';
	import { getContext, onMount, untrack } from 'svelte';
	import TextInputWithLabel from '$lib/components/form/text-input-with-label.svelte';
	import { SettingsPageLayout, type SettingsActionButton } from '$lib/layouts/index.js';
	import settingsStore from '$lib/stores/config-store';
	import { Label } from '$lib/components/ui/label';
	import { m } from '$lib/paraglide/messages';
	import { notificationService } from '$lib/services/notification-service';
	import type { NotificationSettings, AppriseSettings } from '$lib/types/notification.type';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { NotificationsIcon, SendEmailIcon, AddIcon } from '$lib/icons';
	import ProvidersTable from './components/providers-table.svelte';
	import AddNotificationProviderSheet from '$lib/components/sheets/add-notification-provider-sheet.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';

	let { data } = $props();
	let isLoading = $state(false);
	let isTesting = $state(false);
	let showUnsavedDialog = $state(false);
	let pendingTestAction: (() => Promise<void>) | null = $state(null);
	const isReadOnly = $derived.by(() => $settingsStore.uiConfigDisabled);
	const formState = getContext('settingsFormState') as any;

	let activeTab = $state('providers');

	let providers = $state<Paginated<NotificationSettings>>({
		data: untrack(() => data.notificationSettings),
		pagination: {
			currentPage: 1,
			totalPages: 1,
			totalItems: untrack(() => data.notificationSettings.length),
			itemsPerPage: 100
		}
	});

	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 100 }
	});

	let selectedIds = $state<string[]>([]);
	let isSheetOpen = $state(false);
	let providerToEdit = $state<NotificationSettings | null>(null);

	let appriseSettings: AppriseSettings = $state(
		untrack(() => data.appriseSettings) || {
			apiUrl: '',
			enabled: false,
			imageUpdateTag: '',
			containerUpdateTag: ''
		}
	);

	let savedAppriseSettings: AppriseSettings = $state(
		untrack(() => data.appriseSettings)
			? { ...untrack(() => data.appriseSettings) }
			: {
					apiUrl: '',
					enabled: false,
					imageUpdateTag: '',
					containerUpdateTag: ''
				}
	);

	const hasChanges = $derived(
		appriseSettings.enabled !== savedAppriseSettings.enabled ||
			appriseSettings.apiUrl !== savedAppriseSettings.apiUrl ||
			appriseSettings.imageUpdateTag !== savedAppriseSettings.imageUpdateTag ||
			appriseSettings.containerUpdateTag !== savedAppriseSettings.containerUpdateTag
	);

	$effect(() => {
		if (formState) {
			formState.hasChanges = activeTab === 'legacy' ? hasChanges : false;
			formState.isLoading = isLoading;
			formState.saveFunction = activeTab === 'legacy' ? onSaveApprise : null;
			formState.resetFunction = activeTab === 'legacy' ? resetForm : null;
		}
	});

	async function loadProviders() {
		try {
			const data = await notificationService.getSettings();
			providers = {
				data: data,
				pagination: {
					currentPage: 1,
					totalPages: 1,
					totalItems: data.length,
					itemsPerPage: 100
				}
			};
		} catch (error) {
			toast.error('Failed to load notification providers');
		}
	}

	async function onSaveApprise() {
		isLoading = true;
		try {
			await notificationService.updateAppriseSettings(appriseSettings);
			savedAppriseSettings = { ...appriseSettings };
			toast.success(m.general_settings_saved());
		} catch (error: any) {
			const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
			toast.error(m.notifications_saved_failed({ provider: 'Apprise', error: errorMsg }));
		} finally {
			isLoading = false;
		}
	}

	function resetForm() {
		appriseSettings = { ...savedAppriseSettings };
	}

	async function handleProviderSubmit(providerData: NotificationSettings) {
		isLoading = true;
		try {
			await notificationService.updateSettings(providerData.provider, providerData);
			toast.success(m.general_settings_saved());
			isSheetOpen = false;
			await loadProviders();
		} catch (error: any) {
			toast.error(error?.response?.data?.error || 'Failed to save provider');
		} finally {
			isLoading = false;
		}
	}

	function openAddSheet() {
		providerToEdit = null;
		isSheetOpen = true;
	}

	function openEditSheet(provider: NotificationSettings) {
		providerToEdit = provider;
		isSheetOpen = true;
	}

	async function testProvider(provider: NotificationSettings) {
		isTesting = true;
		try {
			await notificationService.testNotification(provider.provider);
			toast.success(m.notifications_test_success({ provider: provider.name }));
		} catch (error: any) {
			const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
			toast.error(m.notifications_test_failed({ error: errorMsg }));
		} finally {
			isTesting = false;
		}
	}

	async function testAppriseNotification() {
		isTesting = true;
		try {
			await notificationService.testAppriseNotification();
			toast.success(m.notifications_test_success({ provider: 'Apprise' }));
		} catch (error: any) {
			const errorMsg = error?.response?.data?.error || error.message || m.common_unknown();
			toast.error(m.notifications_test_failed({ error: errorMsg }));
		} finally {
			isTesting = false;
		}
	}

	async function handleSaveAndTest() {
		showUnsavedDialog = false;
		await onSaveApprise();
		if (pendingTestAction) {
			await pendingTestAction();
			pendingTestAction = null;
		}
	}

	const actionButtons: SettingsActionButton[] = $derived.by(() => {
		if (activeTab === 'providers') {
			return [
				{
					id: 'add-provider',
					action: 'create',
					label: m.common_add_button({ resource: 'Provider' }),
					onclick: openAddSheet,
					disabled: isReadOnly
				}
			];
		}
		return [];
	});
</script>

<SettingsPageLayout
	title={m.notifications_title()}
	description={m.notifications_description()}
	icon={NotificationsIcon}
	pageType={activeTab === 'legacy' ? 'form' : 'management'}
	showReadOnlyTag={isReadOnly}
	{actionButtons}
>
	{#snippet mainContent()}
		<fieldset disabled={isReadOnly} class="relative h-full">
			{#if isReadOnly}
				<Alert.Root variant="default" class="mb-4 sm:mb-6">
					<Alert.Title>{m.notifications_read_only_title()}</Alert.Title>
					<Alert.Description>{m.notifications_read_only_description()}</Alert.Description>
				</Alert.Root>
			{/if}

			<Tabs.Root bind:value={activeTab} class="flex h-full flex-col">
				<div class="flex items-center justify-between gap-4">
					<Tabs.List class="inline-flex w-auto shrink-0">
						<Tabs.Trigger value="providers">Providers</Tabs.Trigger>
						<Tabs.Trigger value="legacy">Legacy</Tabs.Trigger>
					</Tabs.List>
				</div>

				<Tabs.Content value="providers" class="mt-4 flex-1 overflow-hidden sm:mt-6">
					<ProvidersTable bind:providers bind:selectedIds bind:requestOptions onEdit={openEditSheet} onTest={testProvider} />
				</Tabs.Content>

				<Tabs.Content value="legacy" class="mt-4 space-y-4 sm:mt-6 sm:space-y-6">
					<div class="space-y-4">
						<h3 class="text-lg font-medium">Apprise</h3>
						<div class="bg-card rounded-lg border shadow-sm">
							<div class="space-y-6 p-6">
								<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
									<div>
										<Label class="text-base">{m.notifications_apprise_title()}</Label>
										<p class="text-muted-foreground mt-1 text-sm">{m.notifications_apprise_description()}</p>
									</div>
									<div class="space-y-4">
										<div class="flex items-center gap-2">
											<Switch
												id="apprise-enabled"
												bind:checked={appriseSettings.enabled}
												disabled={isReadOnly}
												onCheckedChange={(checked: boolean) => {
													appriseSettings.enabled = checked;
												}}
											/>
											<Label for="apprise-enabled" class="font-normal">
												{m.notifications_apprise_enabled_label()}
											</Label>
										</div>

										{#if appriseSettings.enabled}
											<div class="space-y-4 pt-2">
												<TextInputWithLabel
													bind:value={appriseSettings.apiUrl}
													disabled={isReadOnly}
													label={m.notifications_apprise_api_url_label()}
													placeholder={m.notifications_apprise_api_url_placeholder()}
													type="url"
													autocomplete="off"
													helpText={m.notifications_apprise_api_url_help()}
												/>
												<TextInputWithLabel
													bind:value={appriseSettings.imageUpdateTag}
													disabled={isReadOnly}
													label={m.notifications_apprise_image_tag_label()}
													placeholder={m.notifications_apprise_image_tag_placeholder()}
													type="text"
													autocomplete="off"
													helpText={m.notifications_apprise_image_tag_help()}
												/>
												<TextInputWithLabel
													bind:value={appriseSettings.containerUpdateTag}
													disabled={isReadOnly}
													label={m.notifications_apprise_container_tag_label()}
													placeholder={m.notifications_apprise_container_tag_placeholder()}
													type="text"
													autocomplete="off"
													helpText={m.notifications_apprise_container_tag_help()}
												/>

												<div class="pt-2">
													<ArcaneButton
														action="base"
														tone="outline"
														onclick={() => testAppriseNotification()}
														disabled={isReadOnly || isTesting}
														loading={isTesting}
														icon={SendEmailIcon}
														customLabel={m.notifications_apprise_test_button()}
													/>
												</div>
											</div>
										{/if}
									</div>
								</div>
							</div>
						</div>
					</div>
				</Tabs.Content>
			</Tabs.Root>
		</fieldset>
	{/snippet}
</SettingsPageLayout>

<AddNotificationProviderSheet bind:open={isSheetOpen} bind:providerToEdit onSubmit={handleProviderSubmit} {isLoading} />

<Dialog.Root bind:open={showUnsavedDialog}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>{m.notifications_unsaved_changes_title()}</Dialog.Title>
			<Dialog.Description>
				{m.notifications_unsaved_changes_description()}
			</Dialog.Description>
		</Dialog.Header>
		<Dialog.Footer>
			<ArcaneButton action="cancel" onclick={() => (showUnsavedDialog = false)} />
			<ArcaneButton action="confirm" onclick={handleSaveAndTest} customLabel={m.notifications_unsaved_changes_save_and_test()} />
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

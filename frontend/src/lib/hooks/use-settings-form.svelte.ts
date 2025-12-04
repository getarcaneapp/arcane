import { getContext } from 'svelte';
import settingsStore from '$lib/stores/config-store';
import { settingsService } from '$lib/services/settings-service';
import type { Settings } from '$lib/types/settings.type';
import { tryCatch } from '$lib/utils/try-catch';
import type { Readable } from 'svelte/store';

type SettingsFormState = {
	hasChanges: boolean;
	isLoading: boolean;
	saveFunction?: () => Promise<void> | void;
	resetFunction?: () => void;
};

type Options<T> = {
	formInputs: Readable<T>;
	getCurrentSettings: () => Record<string, any>;
};

export class UseSettingsForm<T extends Record<string, { value: any; error: string | null }>> {
	#hasChanges = $state(false);
	#isLoading = $state(false);
	#formValues = $state<T | null>(null);
	private formState: SettingsFormState | undefined;
	private getCurrentSettings: () => Record<string, any>;

	constructor({ formInputs, getCurrentSettings }: Options<T>) {
		this.getCurrentSettings = getCurrentSettings;

		try {
			this.formState = getContext('settingsFormState') as SettingsFormState | undefined;
		} catch {
			// Context not available
		}

		// Subscribe to form inputs store to track changes
		formInputs.subscribe((value) => {
			this.#formValues = value;
		});

		$effect(() => {
			// Access reactive state to trigger re-runs
			const currentFormValues = this.#formValues;
			if (!currentFormValues) {
				this.#hasChanges = false;
				return;
			}

			const settingsToCompare = this.getCurrentSettings();
			const keys = Object.keys(currentFormValues) as (keyof T)[];

			const hasChanges = keys.some((key) => {
				const input = currentFormValues[key];
				if (input && 'value' in input) {
					return input.value !== settingsToCompare[key as string];
				}
				return false;
			});

			this.#hasChanges = hasChanges;

			if (this.formState) {
				this.formState.hasChanges = hasChanges;
				this.formState.isLoading = this.#isLoading;
			}
		});
	}

	async updateSettings(updatedSettings: Partial<Settings>) {
		const result = await tryCatch(settingsService.updateSettings(updatedSettings as any));

		if (result.error) {
			console.error('Error updating settings:', result.error);
			throw result.error;
		}

		await settingsStore.reload();
	}

	registerFormActions(saveFunction: () => Promise<void> | void, resetFunction: () => void) {
		if (this.formState) {
			this.formState.saveFunction = saveFunction;
			this.formState.resetFunction = resetFunction;
		}
	}

	setLoading(loading: boolean) {
		this.#isLoading = loading;
	}

	get hasChanges() {
		return this.#hasChanges;
	}

	get isLoading() {
		return this.#isLoading;
	}
}

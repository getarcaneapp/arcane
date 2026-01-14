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

type SettingsPayload = Partial<Settings> & Record<string, unknown>;

type Options<TFormInputs, TSaveData extends SettingsPayload> = {
	formInputs: Readable<TFormInputs>;
	getCurrentSettings: () => TSaveData;
	/**
	 * Custom save handler. If provided, this will be called instead of the default
	 * settingsService.updateSettings(). Useful for environment-specific settings.
	 */
	onSave?: (data: TSaveData) => Promise<void>;
};

export class UseSettingsForm<
	TFormInputs extends Record<string, { value: unknown; error: string | null }>,
	TSaveData extends SettingsPayload
> {
	#isLoading = $state(false);
	#formValues = $state<TFormInputs | null>(null);
	#saveFunction: (() => Promise<void> | void) | null = null;
	#resetFunction: (() => void) | null = null;
	private formState: SettingsFormState | undefined;
	private getCurrentSettings: () => TSaveData;
	private customOnSave?: (data: TSaveData) => Promise<void>;

	constructor({ formInputs, getCurrentSettings, onSave }: Options<TFormInputs, TSaveData>) {
		this.getCurrentSettings = getCurrentSettings;
		this.customOnSave = onSave;

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
			// Sync to external context (side effect)
			if (this.formState) {
				this.formState.hasChanges = this.hasChanges;
				this.formState.isLoading = this.#isLoading;
				if (this.#saveFunction) this.formState.saveFunction = this.#saveFunction;
				if (this.#resetFunction) this.formState.resetFunction = this.#resetFunction;
			}
		});
	}

	#hasChanges = $derived.by(() => {
		const currentFormValues = this.#formValues;
		if (!currentFormValues) return false;

		const settingsToCompare = this.getCurrentSettings();
		const keys = Object.keys(currentFormValues) as (keyof TFormInputs)[];

		return keys.some((key) => {
			const input = currentFormValues[key];
			if (input && 'value' in input) {
				return input.value !== settingsToCompare[key as string];
			}
			return false;
		});
	});

	async updateSettings(updatedSettings: Partial<TSaveData>) {
		// Use custom save handler if provided
		if (this.customOnSave) {
			const mergedSettings = {
				...this.getCurrentSettings(),
				...updatedSettings
			} as TSaveData;
			await this.customOnSave(mergedSettings);
		} else {
			const result = await tryCatch(settingsService.updateSettings(updatedSettings));

			if (result.error) {
				console.error('Error updating settings:', result.error);
				throw result.error;
			}
		}

		await settingsStore.reload();
	}

	registerFormActions(saveFunction: () => Promise<void> | void, resetFunction: () => void) {
		this.#saveFunction = saveFunction;
		this.#resetFunction = resetFunction;
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

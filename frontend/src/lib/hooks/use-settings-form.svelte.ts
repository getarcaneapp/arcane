import { onDestroy } from 'svelte';

import { getSettingsFormContext, hasSettingsFormContext } from '#lib/hooks/settings-form-context.js';
import settingsStore from '#lib/stores/config-store.svelte.js';
import type { Settings } from '#lib/types/settings.js';
import type { SettingsFormContext, SettingsFormState } from '#lib/types/ui/settingsform.js';

type SettingsPayload = Partial<Settings> & Record<string, unknown>;

type Options<TFormInputs, TSaveData extends SettingsPayload> = {
	formInputs: () => TFormInputs;
	getCurrentSettings: () => TSaveData;
	onSave?: (data: TSaveData) => Promise<void>;
};

export class UseSettingsForm<
	TFormInputs extends Record<string, { value: unknown; error: string | null }>,
	TSaveData extends SettingsPayload
> implements SettingsFormState {
	#isLoading = $state(false);
	#hasSaved = $state(false);
	#formValues: () => TFormInputs;
	#saveFunction: (() => Promise<void> | void) | null = null;
	#resetFunction: (() => void) | null = null;
	private formContext: SettingsFormContext | undefined;
	private getCurrentSettings: () => TSaveData;
	private customOnSave?: (data: TSaveData) => Promise<void>;

	constructor({ formInputs, getCurrentSettings, onSave }: Options<TFormInputs, TSaveData>) {
		this.getCurrentSettings = getCurrentSettings;
		this.customOnSave = onSave;
		this.#formValues = formInputs;

		this.formContext = hasSettingsFormContext() ? getSettingsFormContext() : undefined;

		if (this.formContext) {
			onDestroy(() => this.formContext?.release(this));
		}
	}

	#hasChanges = $derived.by(() => {
		const currentFormValues = this.#formValues();

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
		if (this.customOnSave) {
			await this.customOnSave({ ...this.getCurrentSettings(), ...updatedSettings });
		} else {
			await settingsStore.save(updatedSettings);
		}
		this.#hasSaved = true;
	}

	registerFormActions(saveFunction: () => Promise<void> | void, resetFunction: () => void) {
		this.#saveFunction = saveFunction;
		this.#resetFunction = resetFunction;
		this.formContext?.register(this);
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

	get hasSaved() {
		return this.#hasSaved;
	}

	// fallow-ignore-next-line unused-class-member
	get saveFunction() {
		return this.#saveFunction ?? undefined;
	}

	// fallow-ignore-next-line unused-class-member
	get resetFunction() {
		return this.#resetFunction ?? undefined;
	}
}

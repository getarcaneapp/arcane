import { z } from 'zod/v4';
import { createForm } from '$lib/utils/form.utils';
import { UseSettingsForm } from '$lib/hooks/use-settings-form.svelte';
import { toast } from 'svelte-sonner';
import { get, type Writable } from 'svelte/store';

type FormInput<T> = { value: T; error: string | null };
type FormInputs<T> = { [K in keyof T]: FormInput<T[K]> };

export interface SettingsFormConfig<T extends z.ZodType<any, any>> {
	schema: T;
	currentSettings: z.infer<T>;
	onSuccess?: () => void;
	onReset?: () => void;
	successMessage?: string;
	errorMessage?: string;
}

/**
 * Creates a complete settings form with automatic change detection and save/reset handling.
 * 
 * Usage:
 * ```ts
 * const { formInputs, form, settingsForm, registerOnMount } = createSettingsForm({
 *   schema: formSchema,
 *   currentSettings,
 *   successMessage: m.general_settings_saved(),
 *   onReset: () => applyAccentColor(currentSettings.accentColor)
 * });
 * 
 * onMount(() => registerOnMount());
 * ```
 */
export function createSettingsForm<T extends z.ZodType<any, any>>(config: SettingsFormConfig<T>) {
	const { 
		schema, 
		currentSettings, 
		onSuccess, 
		onReset,
		successMessage = 'Settings saved', 
		errorMessage = 'Failed to save settings' 
	} = config;

	const { inputs: formInputs, ...form } = createForm(schema, currentSettings);

	const hasChangesChecker = () => {
		const current = get(formInputs);
		const keys = Object.keys(currentSettings) as (keyof z.infer<T>)[];
		return keys.some(key => {
			const input = current[key];
			if (input && 'value' in input) {
				return input.value !== currentSettings[key];
			}
			return false;
		});
	};

	const settingsForm = new UseSettingsForm({ hasChangesChecker });

	const onSubmit = async () => {
		const data = form.validate();
		if (!data) {
			toast.error('Please check the form for errors');
			return;
		}
		settingsForm.setLoading(true);

		try {
			await settingsForm.updateSettings(data);
			toast.success(successMessage);
			onSuccess?.();
		} catch (error) {
			console.error('Failed to save settings:', error);
			toast.error(errorMessage);
		} finally {
			settingsForm.setLoading(false);
		}
	};

	const resetForm = () => {
		form.reset();
		onReset?.();
	};

	const registerOnMount = () => {
		settingsForm.registerFormActions(onSubmit, resetForm);
	};

	return {
		formInputs: formInputs as Writable<FormInputs<z.infer<T>>>,
		form,
		settingsForm,
		onSubmit,
		resetForm,
		registerOnMount
	};
}

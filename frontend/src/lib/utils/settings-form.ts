import { tryCatch } from '#lib/utils/try-catch.js';
import { toast } from 'svelte-sonner';
import { z } from 'zod/v4';
import { UseSettingsForm } from '#lib/hooks/use-settings-form.svelte.js';
import { m } from '#lib/paraglide/messages.js';
import type { Settings } from '#lib/types/settings.js';
import { createForm } from '#lib/utils/settings.svelte.js';

type SettingsPayload = Partial<Settings> & Record<string, unknown>;

export interface SettingsFormConfig<T extends z.ZodType<SettingsPayload>> {
	schema: T;
	currentSettings: z.infer<T>;
	getCurrentSettings?: () => z.infer<T>;
	/**
	 * Custom save handler. If provided, this will be called instead of the default
	 * settingsService.updateSettings(). Useful for environment-specific settings
	 * or other custom save logic.
	 */
	onSave?: (data: z.infer<T>) => Promise<void>;
	onSuccess?: () => void;
	onReset?: () => void;
	successMessage?: string;
	errorMessage?: string;
}

export function createSettingsForm<T extends z.ZodType<SettingsPayload>>(config: SettingsFormConfig<T>) {
	const {
		schema,
		currentSettings,
		getCurrentSettings,
		onSave,
		onSuccess,
		onReset,
		successMessage = m.common_update_success({ resource: m.settings() }),
		errorMessage = m.common_update_failed({ resource: m.settings() })
	} = config;

	const form = createForm(schema, currentSettings);
	const formInputs = form.inputs;

	const settingsForm = new UseSettingsForm({
		formInputs: () => formInputs,
		getCurrentSettings: getCurrentSettings ?? (() => currentSettings),
		onSave
	});

	const onSubmit = async () => {
		const data = form.validate();
		if (!data) {
			toast.error(m.security_form_validation_error());
			return;
		}
		settingsForm.setLoading(true);

		try {
			const operationResult = await tryCatch(
				(async () => {
					await settingsForm.updateSettings(data);
					toast.success(successMessage);
					onSuccess?.();
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;
				console.error('Failed to save settings:', error);
				const message = error instanceof Error ? error.message : errorMessage;
				toast.error(message);
			}
		} finally {
			settingsForm.setLoading(false);
		}
	};

	const resetForm = () => {
		form.reset(getCurrentSettings?.() ?? currentSettings);
		onReset?.();
	};

	settingsForm.registerFormActions(onSubmit, resetForm);

	return {
		formInputs,
		form,
		settingsForm,
		onSubmit,
		resetForm
	};
}

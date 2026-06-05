import { m } from '$lib/paraglide/messages';
import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
import { toast } from 'svelte-sonner';
import { z } from 'zod/v4';

export type TemplateEditorValidationState = {
	composeValidationReady: boolean;
	envValidationReady: boolean;
	composeHasErrors: boolean;
	envHasErrors: boolean;
};

type ResetField<T> = {
	set: (value: T) => void;
	value: T;
};

type ValidationResult<T> = T | undefined | false | null;

type RunTemplateEditorSaveOptions<TValidated, TResult> = {
	validationState: TemplateEditorValidationState;
	validate: () => ValidationResult<TValidated>;
	save: (validated: TValidated) => Promise<TResult>;
	failureMessage: string;
	setLoading: (value: boolean) => void;
	onSuccess: (validated: TValidated, result: TResult) => void | Promise<void>;
};

export function createNamedTemplateSchema() {
	return z.object({
		name: z.string().min(1, m.templates_template_name_required()),
		description: z.string().optional().default(''),
		composeContent: z.string().min(1, m.templates_content_required()),
		envContent: z.string().optional().default('')
	});
}

export function createTemplateContentSchema() {
	return z.object({
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});
}

export function getTemplateEditorValidationState(
	composeValidationReady: boolean,
	envValidationReady: boolean,
	composeHasErrors: boolean,
	envHasErrors: boolean
): TemplateEditorValidationState {
	return {
		composeValidationReady,
		envValidationReady,
		composeHasErrors,
		envHasErrors
	};
}

export function hasTemplateEditorErrors(state: TemplateEditorValidationState): boolean {
	return !state.composeValidationReady || !state.envValidationReady || state.composeHasErrors || state.envHasErrors;
}

export function validateTemplateEditorForm<T>(
	state: TemplateEditorValidationState,
	validate: () => ValidationResult<T>
): T | null {
	if (hasTemplateEditorErrors(state)) {
		toast.error(m.templates_validation_error());
		return null;
	}

	const validated = validate();
	if (!validated) {
		toast.error(m.templates_validation_error());
		return null;
	}

	return validated;
}

export async function runTemplateEditorSave<TValidated, TResult>({
	validationState,
	validate,
	save,
	failureMessage,
	setLoading,
	onSuccess
}: RunTemplateEditorSaveOptions<TValidated, TResult>) {
	const validated = validateTemplateEditorForm(validationState, validate);
	if (!validated) return;

	handleApiResultWithCallbacks({
		result: await tryCatch(save(validated)),
		message: failureMessage,
		setLoadingState: setLoading,
		onSuccess: async (result) => {
			await onSuccess(validated, result);
		}
	});
}

export function resetTemplateEditorFields<T>(fields: ResetField<T>[]) {
	for (const field of fields) {
		field.set(field.value);
	}

	toast.info(m.templates_reset_success());
}

import { arcaneButtonVariants, actionConfigs } from '$lib/components/arcane-button/variants';
import { m } from '$lib/paraglide/messages';
import { templateService } from '$lib/services/template-service.js';
import type { Template } from '$lib/types/swarm';
import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
import { toast } from 'svelte-sonner';
import { z } from 'zod/v4';

export type ConvertedDockerRun = {
	dockerCompose: string;
	envVars: string;
	serviceName: string;
};

type ComposeFieldAccessors = {
	getName: () => string | undefined;
	setName: (value: string) => void;
	setComposeContent: (value: string) => void;
	setEnvContent: (value: string) => void;
};

type ComposeFormInputs = {
	name: { value: string };
	composeContent: { value: string };
	envContent: { value: string };
};

type ComposeFieldKey = keyof ComposeFormInputs;
type ComposeFieldSetter = (key: ComposeFieldKey, value: string) => void;

export const templateBtnClass = arcaneButtonVariants({
	tone: actionConfigs.template?.tone ?? 'outline-primary',
	size: 'default',
	hoverEffect: 'none'
});

export const dropdownContentClass =
	'arcane-dd-content min-w-[220px] overflow-visible rounded-lg border border-primary/30 bg-background/95 ' +
	'backdrop-blur supports-[backdrop-filter]:bg-background/80 ring-1 ring-inset ring-primary/20 shadow-sm p-1';

export const dropdownItemClass =
	'flex cursor-pointer select-none items-center gap-2 rounded-md px-3 py-2 text-sm ' +
	'text-foreground/90 outline-none transition-colors ' +
	'hover:bg-primary/10 focus:bg-primary/10 ' +
	'data-[disabled]:opacity-50 data-[disabled]:pointer-events-none';

export function templateNameSlug(name: string): string {
	return name.toLowerCase().replace(/[^a-z0-9-_]/g, '-');
}

export function createComposeEditorSchema(nameRequiredMessage: string) {
	return z.object({
		name: z
			.string()
			.min(1, nameRequiredMessage)
			.regex(/^[a-z0-9-_]+$/i, m.compose_project_name_invalid()),
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});
}

function createComposeFieldAccessors(
	getInputs: () => ComposeFormInputs,
	setInputValue: ComposeFieldSetter
): ComposeFieldAccessors {
	return {
		getName: () => getInputs().name.value,
		setName: (value: string) => setInputValue('name', value),
		setComposeContent: (value: string) => setInputValue('composeContent', value),
		setEnvContent: (value: string) => setInputValue('envContent', value)
	};
}

function applyTemplateToComposeFields(template: Template, fields: ComposeFieldAccessors) {
	fields.setComposeContent(template.content ?? '');
	fields.setEnvContent(template.envContent ?? '');

	if (!fields.getName()?.trim()) {
		fields.setName(templateNameSlug(template.name));
	}
	toast.success(m.compose_template_loaded({ name: template.name }));
}

function applyConvertedDockerRunToComposeFields(data: ConvertedDockerRun, fields: ComposeFieldAccessors) {
	fields.setComposeContent(data.dockerCompose);
	fields.setEnvContent(data.envVars);
	if (!fields.getName()?.trim()) {
		fields.setName(templateNameSlug(data.serviceName));
	}
}

function createComposeFieldHandlers(fields: ComposeFieldAccessors, closeTemplateDialog: () => void) {
	return {
		handleTemplateSelect(template: Template) {
			closeTemplateDialog();
			applyTemplateToComposeFields(template, fields);
		},
		handleDockerRunConverted(data: ConvertedDockerRun) {
			applyConvertedDockerRunToComposeFields(data, fields);
		}
	};
}

type ValidatedComposeTemplate = {
	name: string;
	composeContent: string;
	envContent?: string;
};

type SubmitComposeResourceOptions<TResult> = {
	validate: () => ValidatedComposeTemplate | undefined | false | null;
	setLoading: (value: boolean) => void;
	submit: (payload: ValidatedComposeTemplate) => Promise<TResult>;
	failureMessage: (name: string) => string;
	onSuccess: (result: TResult, payload: ValidatedComposeTemplate) => void | Promise<void>;
};

type CreateComposeTemplateOptions = {
	validate: () => ValidatedComposeTemplate | undefined | false | null;
	setLoading: (value: boolean) => void;
	beforeValidate?: () => boolean;
};

type CreateComposeTemplateWithValidationOptions = Omit<CreateComposeTemplateOptions, 'beforeValidate'> & {
	hasEditorErrors?: () => boolean;
};

type CreateComposeTemplateDialogFlowOptions = CreateComposeTemplateWithValidationOptions & {
	getInputs: () => ComposeFormInputs;
	setInputValue: ComposeFieldSetter;
	closeTemplateDialog: () => void;
};

async function createComposeTemplate({ validate, setLoading, beforeValidate }: CreateComposeTemplateOptions) {
	if (beforeValidate && !beforeValidate()) return;

	const validated = validate();
	if (!validated) return;

	const { name, composeContent, envContent } = validated;

	handleApiResultWithCallbacks({
		result: await tryCatch(
			templateService.createTemplate({
				name,
				content: composeContent,
				envContent
			})
		),
		message: m.common_create_failed({ resource: `${m.resource_template()} "${name}"` }),
		setLoadingState: setLoading,
		onSuccess: async () => {
			toast.success(m.common_create_success({ resource: `${m.resource_template()} "${name}"` }));
		}
	});
}

export async function submitComposeResourceForm<TResult>({
	validate,
	setLoading,
	submit,
	failureMessage,
	onSuccess
}: SubmitComposeResourceOptions<TResult>) {
	const validated = validate();
	if (!validated) return;

	handleApiResultWithCallbacks({
		result: await tryCatch(submit(validated)),
		message: failureMessage(validated.name),
		setLoadingState: setLoading,
		onSuccess: async (result) => {
			await onSuccess(result, validated);
		}
	});
}

async function createComposeTemplateWithValidation({
	validate,
	setLoading,
	hasEditorErrors
}: CreateComposeTemplateWithValidationOptions) {
	await createComposeTemplate({
		validate,
		setLoading,
		beforeValidate: hasEditorErrors
			? () => {
					if (!hasEditorErrors()) return true;
					toast.error(m.templates_validation_error());
					return false;
				}
			: undefined
	});
}

export function createComposeTemplateDialogFlow({
	getInputs,
	setInputValue,
	closeTemplateDialog,
	validate,
	setLoading,
	hasEditorErrors
}: CreateComposeTemplateDialogFlowOptions) {
	return {
		composeHandlers: createComposeFieldHandlers(createComposeFieldAccessors(getInputs, setInputValue), closeTemplateDialog),
		handleCreateTemplate: () => createComposeTemplateWithValidation({ validate, setLoading, hasEditorErrors })
	};
}

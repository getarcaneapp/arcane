import { z } from 'zod/v4';
import type { FormInputs } from '#lib/types/form.js';

const LOCAL_SETTING_KEYS = new Set([
	'avatarMaxUploadSizeMb',
	'authLocalEnabled',
	'authSessionTimeout',
	'authPasswordPolicy',
	'authOidcConfig',
	'oidcEnabled',
	'oidcMergeAccounts',
	'oidcSkipTlsVerify',
	'oidcAutoRedirectToProvider',
	'oidcClientId',
	'oidcClientSecret',
	'oidcIssuerUrl',
	'oidcScopes',
	'oidcGroupsClaim',
	'oidcProviderName',
	'oidcProviderLogoUrl',
	'edgeMTLSManagerCAAvailable',
	'experimentalFeaturesEnabled',
	'apnsEnabled'
]);

export function isLocalSetting(key: string): boolean {
	return LOCAL_SETTING_KEYS.has(key);
}

export function extractLocalSettings<T extends object>(settings: T): Partial<T> {
	return Object.fromEntries(Object.entries(settings).filter(([key]) => isLocalSetting(key))) as Partial<T>;
}

export function extractEnvironmentSettings<T extends object>(settings: T): Partial<T> {
	return Object.fromEntries(Object.entries(settings).filter(([key]) => !isLocalSetting(key))) as Partial<T>;
}

export function preventDefault<T extends Event>(fn: (event: T) => unknown) {
	return (event: T) => {
		event.preventDefault();
		fn(event);
	};
}

export function createForm<T extends z.ZodType<Record<string, unknown>>>(schema: T, initialValues: z.infer<T>) {
	const inputs = $state<FormInputs<z.infer<T>>>(initializeInputs(initialValues));
	let errors = $state.raw<z.ZodError<z.infer<T>>>();

	function initializeInputs(values: z.infer<T>): FormInputs<z.infer<T>> {
		const fields = {} as FormInputs<z.infer<T>>;
		const schemaShape = schema instanceof z.ZodObject ? schema.shape : {};
		for (const key of Object.keys(schemaShape) as (keyof z.infer<T>)[]) {
			if (Object.prototype.hasOwnProperty.call(values, key)) {
				fields[key] = { value: values[key], error: null };
			}
		}
		return fields;
	}

	function validate() {
		const values = Object.fromEntries(Object.entries(inputs).map(([key, input]) => [key, input.value]));
		const result = schema.safeParse(values);
		errors = result.error;
		for (const key of Object.keys(inputs) as (keyof z.infer<T>)[]) {
			inputs[key].error = result.error?.issues.find((issue) => issue.path[0] === key)?.message ?? null;
		}
		return result.success ? data() : null;
	}

	function data() {
		const values = Object.fromEntries(Object.entries(inputs).map(([key, input]) => [key, trimValue(input.value)]));
		const result = schema.safeParse(values);
		return (result.success ? result.data : values) as z.infer<T>;
	}

	function reset(values: z.infer<T> = initialValues) {
		for (const key of Object.keys(inputs) as (keyof z.infer<T>)[]) {
			inputs[key] = { value: values[key], error: null };
		}
	}

	function setValue<K extends keyof z.infer<T>>(key: K, value: z.infer<T>[K]) {
		inputs[key].value = value;
	}

	function trimValue(value: unknown): unknown {
		if (typeof value === 'string') return value.trim();
		if (Array.isArray(value)) return value.map((item: unknown) => (typeof item === 'string' ? item.trim() : item));
		return value;
	}

	return {
		schema,
		inputs,
		get errors() {
			return errors;
		},
		data,
		validate,
		setValue,
		reset
	};
}

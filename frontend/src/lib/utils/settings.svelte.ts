import { z } from 'zod/v4';

import type { FormInputs } from '#lib/types/form.js';

const LOCAL_SETTING_KEYS = new Set([
	'avatarMaxUploadSizeMb',
	'authLocalEnabled',
	'authSessionTimeout',
	'authPasswordPolicy',
	'authOidcConfig',
	'oidcEnabled',
	'oidcAutoRedirectToProvider',
	'oidcMergeAccounts',
	'oidcSkipTlsVerify',
	'oidcClientId',
	'oidcClientSecret',
	'oidcIssuerUrl',
	'oidcScopes',
	'oidcGroupsClaim',
	'oidcProviderName',
	'oidcProviderLogoUrl',
	'edgeMTLSManagerCAAvailable',
	'frontendTracingEnabled',
	'frontendMetricsEnabled',
	'frontendLogsEnabled',
	'experimentalFeaturesEnabled',
	'developmentBrandingEnabled',
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
	const schemaKeys = Object.keys(schema instanceof z.ZodObject ? schema.shape : {});
	const inputs = $state(
		Object.fromEntries(
			schemaKeys
				.filter((key) => Object.hasOwn(initialValues, key))
				.map((key) => [key, { value: initialValues[key as keyof z.infer<T>], error: null }])
		) as FormInputs<z.infer<T>>
	);
	let errors = $state.raw<z.ZodError<z.infer<T>>>();

	function parse() {
		const values = Object.fromEntries(
			Object.entries(inputs).map(([key, input]) => {
				const value: unknown = input.value;
				if (typeof value === 'string') return [key, value.trim()];
				if (Array.isArray(value)) return [key, value.map((item: unknown) => (typeof item === 'string' ? item.trim() : item))];
				return [key, value];
			})
		);
		return { values, result: schema.safeParse(values) };
	}

	function validate() {
		const { result } = parse();
		errors = result.error;
		for (const key of Object.keys(inputs) as (keyof z.infer<T>)[]) {
			inputs[key].error = result.error?.issues.find((issue) => issue.path[0] === key)?.message ?? null;
		}
		return result.success ? (result.data as z.infer<T>) : null;
	}

	function data() {
		const { values, result } = parse();
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

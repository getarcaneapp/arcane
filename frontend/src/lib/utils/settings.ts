import { get, writable } from 'svelte/store';
import { z } from 'zod/v4';
import type { ApplicationTheme } from '$lib/types/settings';

// --- Local vs environment settings classification ---

export type LocalSettings = {
	applicationTheme: ApplicationTheme;
	accentColor: string;
	oledMode: boolean;
	mobileNavigationMode: string;
	mobileNavigationShowLabels: boolean;
	sidebarHoverExpansion: boolean;
	keyboardShortcutsEnabled: boolean;
	edgeMTLSManagerCAAvailable?: boolean;
};

const LOCAL_SETTING_KEYS = new Set([
	'applicationTheme',
	'accentColor',
	'oledMode',
	'mobileNavigationMode',
	'mobileNavigationShowLabels',
	'sidebarHoverExpansion',
	'keyboardShortcutsEnabled',
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
	'edgeMTLSManagerCAAvailable'
]);

export function isLocalSetting(key: string): boolean {
	return LOCAL_SETTING_KEYS.has(key);
}

export function extractLocalSettings(settings: Record<string, any>): Partial<LocalSettings> {
	const local: Partial<LocalSettings> = {};
	for (const key of LOCAL_SETTING_KEYS) {
		if (key in settings) {
			local[key as keyof LocalSettings] = settings[key];
		}
	}
	return local;
}

export function extractEnvironmentSettings(settings: Record<string, any>): Record<string, any> {
	const env: Record<string, any> = {};
	for (const key in settings) {
		if (!LOCAL_SETTING_KEYS.has(key)) {
			env[key] = settings[key];
		}
	}
	return env;
}

// --- Generic Zod-backed form state ---

export function preventDefault(fn: (event: Event) => any) {
	return function (this: any, event: Event) {
		event.preventDefault();
		fn.call(this, event);
	};
}

export type FormInput<T> = {
	value: T;
	error: string | null;
};

export type FormInputs<T> = {
	[K in keyof T]: FormInput<T[K]>;
};

export function createForm<T extends z.ZodType<any, any>>(schema: T, initialValues: z.infer<T>) {
	const inputsStore = writable<FormInputs<z.infer<T>>>(initializeInputs(initialValues));
	const errorsStore = writable<z.ZodError<any> | undefined>();

	function initializeInputs(initialValues: z.infer<T>): FormInputs<z.infer<T>> {
		const inputs: FormInputs<z.infer<T>> = {} as FormInputs<z.infer<T>>;

		const schemaShape = schema instanceof z.ZodObject ? schema.shape : {};
		const schemaKeys = Object.keys(schemaShape);

		for (const key of schemaKeys) {
			if (Object.prototype.hasOwnProperty.call(initialValues, key)) {
				inputs[key as keyof z.infer<T>] = {
					value: initialValues[key as keyof z.infer<T>],
					error: null
				};
			}
		}
		return inputs;
	}

	function validate() {
		let success = true;
		inputsStore.update((inputs) => {
			const values = Object.fromEntries(Object.entries(inputs).map(([key, input]) => [key, input.value]));

			const result = schema.safeParse(values);
			errorsStore.set(result.error);

			if (!result.success) {
				success = false;
				for (const input of Object.keys(inputs)) {
					const error = result.error.issues.find((e) => e.path[0] === input);
					if (error) {
						inputs[input as keyof z.infer<T>].error = error.message;
					} else {
						inputs[input as keyof z.infer<T>].error = null;
					}
				}
			} else {
				for (const input of Object.keys(inputs)) {
					inputs[input as keyof z.infer<T>].error = null;
				}
			}
			return inputs;
		});
		return success ? data() : null;
	}

	function data() {
		const inputs = get(inputsStore);

		const values = Object.fromEntries(
			Object.entries(inputs).map(([key, input]) => {
				return [key, trimValue(input.value)];
			})
		);

		const result = schema.safeParse(values);
		return (result.success ? result.data : values) as z.infer<T>;
	}

	function reset() {
		inputsStore.update((inputs) => {
			for (const input of Object.keys(inputs)) {
				inputs[input as keyof z.infer<T>] = {
					value: initialValues[input as keyof z.infer<T>],
					error: null
				};
			}
			return inputs;
		});
	}

	function setValue(key: keyof z.infer<T>, value: z.infer<T>[keyof z.infer<T>]) {
		inputsStore.update((inputs) => {
			inputs[key].value = value;
			return inputs;
		});
	}

	function trimValue(value: any) {
		if (typeof value === 'string') {
			value = value.trim();
		} else if (Array.isArray(value)) {
			value = value.map((item: any) => {
				if (typeof item === 'string') {
					return item.trim();
				}
				return item;
			});
		}
		return value;
	}

	return {
		schema,
		inputs: inputsStore,
		errors: errorsStore,
		data,
		validate,
		setValue,
		reset
	};
}

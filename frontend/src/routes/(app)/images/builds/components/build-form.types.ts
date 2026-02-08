import type { FormInput } from '$lib/utils/form.utils';
import type { Writable } from 'svelte/store';

export type BuildProviderOption = {
	label: string;
	value: 'local' | 'depot';
	description?: string;
};

export type BuildFormInputs = {
	dockerfile: FormInput<string>;
	tags: FormInput<string>;
	target: FormInput<string>;
	buildArgs: FormInput<string>;
	platforms: FormInput<string>;
	provider: FormInput<'local' | 'depot'>;
	push: FormInput<boolean>;
	load: FormInput<boolean>;
};

export type BuildFormInputsStore = Writable<BuildFormInputs>;

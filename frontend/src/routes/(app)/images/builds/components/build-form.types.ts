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
	labels: FormInput<string>;
	cacheFrom: FormInput<string>;
	cacheTo: FormInput<string>;
	network: FormInput<string>;
	isolation: FormInput<string>;
	shmSize: FormInput<string>;
	ulimits: FormInput<string>;
	entitlements: FormInput<string>;
	privileged: FormInput<boolean>;
	extraHosts: FormInput<string>;
	platforms: FormInput<string>;
	noCache: FormInput<boolean>;
	pull: FormInput<boolean>;
	provider: FormInput<'local' | 'depot'>;
	push: FormInput<boolean>;
	load: FormInput<boolean>;
};

export type BuildFormInputsStore = Writable<BuildFormInputs>;

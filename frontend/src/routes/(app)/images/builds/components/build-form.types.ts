import type { FormInput } from '#lib/types/form.js';

export type SelectOption = {
	label: string;
	value: string;
	description?: string;
};

export type BuildProviderOption = SelectOption & {
	value: 'local' | 'depot';
};

export type BuildFormInputs = {
	dockerfile: FormInput<string>;
	tags: FormInput<string>;
	registryId: FormInput<string>;
	repositoryName: FormInput<string>;
	pushTag: FormInput<string>;
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

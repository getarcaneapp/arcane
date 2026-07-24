import type { Writable } from 'svelte/store';
import type { FormInputs } from '#lib/utils/settings';
import type { Environment, EnvironmentStatus } from '#lib/types/environment';
import type { EnvironmentFormValues } from './environment-form-schema';

export type EnvironmentFormInputs = Writable<FormInputs<EnvironmentFormValues>>;

export interface ConnectionEdgeTabProps {
	environment: Environment;
	currentStatus: EnvironmentStatus;
	showMTLSDownloads: boolean;
	isRegeneratingKey: boolean;
	onRegenerateApiKey: () => void;
}

export interface StorageTabProps {
	formInputs: EnvironmentFormInputs;
}

export interface DockerTabProps {
	formInputs: EnvironmentFormInputs;
	shellSelectValue: string;
	handleShellSelectChange: (value: string) => void;
	shellOptions: { value: string; label: string; description?: string }[];
}

export interface JobsTabProps {
	formInputs: EnvironmentFormInputs;
	environmentId: string;
}

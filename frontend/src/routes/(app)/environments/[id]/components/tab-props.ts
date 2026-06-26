import type { Writable } from 'svelte/store';
import type { FormInputs } from '$lib/utils/settings';
import type { Environment, EnvironmentStatus } from '$lib/types/environment';
import type { EnvironmentFormValues } from './environment-form-schema';

export type EnvironmentFormInputs = Writable<FormInputs<EnvironmentFormValues>>;

export interface GeneralTabProps {
	formInputs: EnvironmentFormInputs;
	environment: Environment;
	currentStatus: EnvironmentStatus;
	isTestingConnection: boolean;
	testConnection: () => void | Promise<void>;
	settingsAvailable: boolean;
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

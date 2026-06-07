import type { Writable } from 'svelte/store';
import type { FormInputs } from '$lib/utils/form.utils';
import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';
import type { AppVersionInformation } from '$lib/types/application-configuration';
import type { EnvironmentFormValues } from './environment-form-schema';

export type EnvironmentFormInputs = Writable<FormInputs<EnvironmentFormValues>>;

export interface DetailsTabProps {
	environment: Environment;
	formInputs: EnvironmentFormInputs;
	currentStatus: EnvironmentStatus;
	isLoadingVersion: boolean;
	remoteVersion: AppVersionInformation | null;
	versionInformation: AppVersionInformation | null | undefined;
	isTestingConnection: boolean;
	testConnection: () => void | Promise<void>;
}

export interface GeneralTabProps {
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

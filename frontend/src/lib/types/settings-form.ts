export interface SettingsFormState {
	readonly hasChanges: boolean;
	readonly isLoading: boolean;
	readonly saveFunction?: () => Promise<void> | void;
	readonly resetFunction?: () => void;
}

export interface SettingsFormContext {
	readonly activeForm: SettingsFormState | undefined;
	register(form: SettingsFormState): void;
	release(form: SettingsFormState): void;
}

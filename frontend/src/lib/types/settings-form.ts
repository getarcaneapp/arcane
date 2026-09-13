export interface SettingsFormState {
	readonly hasChanges: boolean;
	readonly isLoading: boolean;
	readonly saveFunction?: () => Promise<void> | void;
	readonly resetFunction?: () => void;
}

export interface SettingsFormContext {
	activeForm: SettingsFormState | undefined;
}

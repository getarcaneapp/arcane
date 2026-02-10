export type ProviderFieldValue = string | number | boolean | null | undefined;

export type ProviderFieldKey<T extends object> = Extract<keyof T, string>;

export interface ProviderSelectOption {
	value: string;
	label: string;
	description?: string;
}

interface ProviderFieldBase<T extends object> {
	key: ProviderFieldKey<T>;
	id?: string;
	wrapperClass?: string;
	errorKey?: ProviderFieldKey<T>;
}

export interface ProviderInputField<T extends object> extends ProviderFieldBase<T> {
	kind: 'input';
	label: string;
	placeholder?: string;
	helpText?: string;
	inputType?: 'text' | 'email' | 'password' | 'number' | 'url';
	autocomplete?: HTMLInputElement['autocomplete'];
	required?: boolean;
}

export interface ProviderTextareaField<T extends object> extends ProviderFieldBase<T> {
	kind: 'textarea';
	label: string;
	placeholder?: string;
	helpText?: string;
	rows?: number;
	autocomplete?: HTMLTextAreaElement['autocomplete'];
}

export interface ProviderSwitchField<T extends object> extends ProviderFieldBase<T> {
	kind: 'switch';
	label: string;
	description?: string;
}

export interface ProviderSelectField<T extends object> extends ProviderFieldBase<T> {
	kind: 'select';
	label: string;
	placeholder?: string;
	description?: string;
	valueType?: 'string' | 'number';
	options: ProviderSelectOption[];
}

export interface ProviderNativeSelectField<T extends object> extends ProviderFieldBase<T> {
	kind: 'native-select';
	label: string;
	description?: string;
	valueType?: 'string' | 'number';
	options: ProviderSelectOption[];
}

export type ProviderFormField<T extends object> =
	| ProviderInputField<T>
	| ProviderTextareaField<T>
	| ProviderSwitchField<T>
	| ProviderSelectField<T>
	| ProviderNativeSelectField<T>;

export interface ProviderFormRow<T extends object> {
	kind: 'row';
	className?: string;
	fields: ProviderFormField<T>[];
}

export type ProviderFormNode<T extends object> = ProviderFormField<T> | ProviderFormRow<T>;

export type ProviderFormSchema<T extends object> = ProviderFormNode<T>[];

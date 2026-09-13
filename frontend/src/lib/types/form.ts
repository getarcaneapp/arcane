export type FormInput<T> = {
	value: T;
	error: string | null;
};

export type FormInputs<T> = {
	[K in keyof T]: FormInput<T[K]>;
};

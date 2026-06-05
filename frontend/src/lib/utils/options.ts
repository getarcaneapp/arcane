function selectedOptionLabel<T extends { id: string; name: string }>(options: T[], value: string, fallback: string): string {
	return options.find((option) => option.id === value)?.name ?? fallback;
}

export const GLOBAL_ENVIRONMENT_OPTION_ID = 'global';

export function buildGlobalEnvironmentOptions<T extends { id: string; name: string }>(
	environments: T[],
	globalLabel: string
): { id: string; name: string }[] {
	return [
		{ id: GLOBAL_ENVIRONMENT_OPTION_ID, name: globalLabel },
		...environments.map((env) => ({ id: env.id, name: env.name }))
	];
}

export function createRoleEnvironmentLabelers<
	TRole extends { id: string; name: string },
	TEnvironment extends { id: string; name: string }
>(roles: TRole[], environments: TEnvironment[], fallback: string) {
	return {
		environment: (value: string) => selectedOptionLabel(environments, value, fallback),
		role: (value: string) => selectedOptionLabel(roles, value, fallback)
	};
}

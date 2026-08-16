import 'unplugin-icons/types/svelte';

// Compat shim: svelte-check-rs (<= 0.11.0) annotates hook exports with
// `import('@sveltejs/kit').HandleClientError`, but SvelteKit 3.0.0-next.20
// moved that type to `@sveltejs/kit/hooks`. Remove once svelte-check-rs
// references the new location.
declare module '@sveltejs/kit' {
	export type HandleClientError = import('@sveltejs/kit/hooks').HandleClientError;
}

declare global {
	namespace App {
		interface Error {
			message: string;
			status?: number;
		}
		// interface Locals {
		// 	user?: User | null;
		// }
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};

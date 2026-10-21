import 'unplugin-icons/types/svelte';
import 'temporal-polyfill/types/global';

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

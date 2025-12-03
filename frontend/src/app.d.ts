import type { User } from '$lib/types/user.type';

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

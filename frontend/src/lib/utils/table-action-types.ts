export type TableActionConfig<TStatus extends string> = {
	status: TStatus;
	run: (id: string) => Promise<unknown>;
	success: () => string;
	failure: () => string;
};

export type TableBulkActionConfig<TLoadingKey extends string> = {
	title: (count: number) => string;
	message: (count: number) => string;
	label: string;
	loadingKey: TLoadingKey;
	run: (id: string) => Promise<unknown>;
	success: (count: number) => string;
	partial: (success: number, total: number, failed: number) => string;
	failure: () => string;
	destructive?: boolean;
};

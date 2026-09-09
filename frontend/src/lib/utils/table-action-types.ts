export type TableActionConfig<TStatus extends string, TTarget = string> = {
	status: TStatus;
	run: (target: TTarget) => Promise<unknown>;
	success: () => string;
	failure: () => string;
};

export type TableBulkActionConfig<TLoadingKey extends string, TTarget = string> = {
	title: (count: number) => string;
	message: (count: number) => string;
	label: string;
	loadingKey: TLoadingKey;
	run: (target: TTarget) => Promise<unknown>;
	success: (count: number) => string;
	partial: (success: number, total: number, failed: number) => string;
	failure: () => string;
	destructive?: boolean;
};

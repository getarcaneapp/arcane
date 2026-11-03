export interface OperationStreamFrame {
	done?: boolean;
	error?: string | { message?: string };
}

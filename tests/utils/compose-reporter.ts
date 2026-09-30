import type { Reporter, TestCase, TestError, TestResult } from '@playwright/test/reporter';
import { captureComposeDiagnostics } from '../setup/global-setup';

export default class ComposeReporter implements Reporter {
	private capturedFailure = false;

	onTestEnd(test: TestCase, result: TestResult) {
		if (
			result.status === test.expectedStatus ||
			result.status === 'skipped' ||
			this.capturedFailure
		)
			return;
		this.capturedFailure = true;
		this.capture();
	}

	onError(_error: TestError) {
		this.capture();
	}

	private capture() {
		const composeFile = process.env.ARCANE_E2E_COMPOSE_FILE;
		if (composeFile) captureComposeDiagnostics(composeFile);
	}
}

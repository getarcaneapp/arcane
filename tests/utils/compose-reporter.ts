import type { Reporter, TestCase } from 'vite-plus/test/node';
import { captureComposeDiagnostics } from '../setup/global-setup';
import { composeFile } from './report';

export default class ComposeReporter implements Reporter {
	private capturedFailure = false;

	onTestCaseResult(testCase: TestCase) {
		if (this.capturedFailure || testCase.result().state !== 'failed') return;
		this.capturedFailure = true;
		captureComposeDiagnostics(composeFile);
	}
}

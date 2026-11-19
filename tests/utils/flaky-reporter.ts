import type { Reporter, TestCase } from 'vite-plus/test/node';

export default class FlakyReporter implements Reporter {
	private flaky: string[] = [];

	onTestCaseResult(testCase: TestCase) {
		if (testCase.diagnostic()?.flaky) this.flaky.push(testCase.fullName);
	}

	onTestRunEnd() {
		if (this.flaky.length === 0) return;
		console.error(`Flaky tests passed only after a retry:\n${this.flaky.join('\n')}`);
		process.exitCode = 1;
	}
}

/**
 * Backs the operation watch dialog: a terminal-style view of a single running
 * Docker operation's raw CLI output. Streamed operations feed it via their
 * NDJSON {"log":...} frames. Interactive operations (docker compose up without
 * -d) attach follow-up log streaming and register an onClose hook that stops
 * the project when the dialog is dismissed — the Ctrl-C equivalent.
 */
function createOperationWatchStore() {
	let _open = $state(false);
	let _title = $state('');
	let _lines = $state<string[]>([]);
	let _error = $state('');
	let _onClose: (() => void) | null = null;
	let _closeRequestHandler: (() => void) | null = null;

	function runCloseHookInternal() {
		const hook = _onClose;
		_onClose = null;
		hook?.();
	}

	return {
		get open() {
			return _open;
		},
		set open(value: boolean) {
			const closing = _open && !value;
			if (closing && _onClose && _closeRequestHandler) {
				// An attached session's close is the Ctrl-C that stops the project —
				// veto the dismissal (X, Escape, outside click) and ask first.
				_closeRequestHandler();
				return;
			}
			_open = value;
			if (closing) {
				runCloseHookInternal();
			}
		},
		get title() {
			return _title;
		},
		get lines() {
			return _lines;
		},
		get error() {
			return _error;
		},

		start(title: string) {
			// Replacing a previous session releases its resources first.
			runCloseHookInternal();
			_title = title;
			_lines = [];
			_error = '';
			_open = true;
		},

		append(line: string) {
			_lines = [..._lines, line];
		},

		/** onLine callback for readNdjsonStream: appends raw log frames. */
		onLine(data: unknown) {
			if (!data || typeof data !== 'object') {
				return;
			}
			const frame = data as { log?: unknown };
			if (typeof frame.log === 'string') {
				_lines = [..._lines, frame.log];
			}
		},

		fail(message: string) {
			_error = message;
			// A failed operation has nothing to stop or detach from.
			_onClose = null;
		},

		/**
		 * Registers cleanup for the current session, invoked exactly once when
		 * the dialog closes (or a new session replaces this one).
		 */
		setOnClose(hook: () => void) {
			_onClose = hook;
		},

		/**
		 * Registers the dialog's confirm-before-close prompt, invoked instead of
		 * closing while a session close hook is armed.
		 */
		setCloseRequestHandler(handler: () => void) {
			_closeRequestHandler = handler;
		},

		close() {
			this.open = false;
		},

		/** Closes without the confirm prompt, running the session close hook. */
		forceClose() {
			_open = false;
			runCloseHookInternal();
		}
	};
}

export const operationWatchStore = createOperationWatchStore();

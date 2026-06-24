const test = require('node:test');
const assert = require('node:assert');
const { analyzeContent } = require('./validate_i18n');

test('validate_i18n logic', async (t) => {
    
    await t.test('detects plain text inside tags', () => {
        const errors = analyzeContent('<div>Hello World</div>', 'test.svelte');
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Hello World/);
    });

    await t.test('detects multi-line plain text inside tags', () => {
        const content = `<div>
            Hello
            World
        </div>`;
        const errors = analyzeContent(content, 'test.svelte');
        assert.strictEqual(errors.length, 2);
        assert.match(errors[0], /Hello/);
        assert.match(errors[1], /World/);
    });

    await t.test('detects partially interpolated strings', () => {
        const errors = analyzeContent('<div>Member since {date}</div>', 'test.svelte');
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Member since/);
    });

    await t.test('ignores valid i18n usage', () => {
        const errors = analyzeContent('<div>{m.hello_world()}</div>', 'test.svelte');
        assert.strictEqual(errors.length, 0);
    });

    await t.test('ignores whitelisted technical tokens', () => {
        const errors = analyzeContent('<div>8080</div>', 'test.svelte');
        assert.strictEqual(errors.length, 0);
    });

    await t.test('ignores Svelte logic blocks', () => {
        const errors = analyzeContent('{#each data.projects as project}', 'test.svelte');
        assert.strictEqual(errors.length, 0);
    });

    await t.test('detects hardcoded attributes', () => {
        const errors = analyzeContent('<button aria-label="Close modal"></button>', 'test.svelte');
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Close modal/);
    });
    
    await t.test('detects custom component labels', () => {
        const errors = analyzeContent('<MyComponent customLabel="Log out"></MyComponent>', 'test.svelte');
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Log out/);
    });

    await t.test('detects blacklisted words even if short', () => {
        const errors = analyzeContent('<button title="Save"></button>', 'test.svelte');
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Save/);
    });

    await t.test('ignores whitelisted attributes', () => {
        const errors = analyzeContent('<input placeholder="value" />', 'test.svelte');
        assert.strictEqual(errors.length, 0);
    });
});

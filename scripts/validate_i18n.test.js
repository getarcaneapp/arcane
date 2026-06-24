const test = require('node:test');
const assert = require('node:assert');
const { analyzeLine } = require('./validate_i18n');

test('validate_i18n logic', async (t) => {
    
    await t.test('detects plain text inside tags', () => {
        const errors = analyzeLine('<div>Hello World</div>', 'test.svelte', 1);
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Hello World/);
    });

    await t.test('ignores valid i18n usage', () => {
        const errors = analyzeLine('<div>{m.hello_world()}</div>', 'test.svelte', 1);
        assert.strictEqual(errors.length, 0);
    });

    await t.test('ignores whitelisted technical tokens', () => {
        const errors = analyzeLine('<div>8080</div>', 'test.svelte', 1);
        assert.strictEqual(errors.length, 0);
    });

    await t.test('ignores Svelte logic blocks', () => {
        const errors = analyzeLine('{#each data.projects as project}', 'test.svelte', 1);
        assert.strictEqual(errors.length, 0);
    });

    await t.test('detects hardcoded attributes', () => {
        const errors = analyzeLine('<button aria-label="Close modal"></button>', 'test.svelte', 1);
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Close modal/);
    });

    await t.test('detects blacklisted words even if short', () => {
        const errors = analyzeLine('<button title="Save"></button>', 'test.svelte', 1);
        assert.strictEqual(errors.length, 1);
        assert.match(errors[0], /Save/);
    });

    await t.test('ignores whitelisted attributes', () => {
        const errors = analyzeLine('<input placeholder="value" />', 'test.svelte', 1);
        assert.strictEqual(errors.length, 0);
    });
});

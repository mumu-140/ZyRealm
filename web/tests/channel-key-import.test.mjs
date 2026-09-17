import assert from 'node:assert/strict';
import test from 'node:test';

const moduleURL = new URL('../src/components/modules/channel/key-import.ts', import.meta.url);

async function loadImportHelpers() {
    return import(moduleURL.href);
}

test('bulk credential parser trims, ignores blanks, de-duplicates, and preserves first-seen order', async () => {
    const { parseCredentialLines } = await loadImportHelpers();
    const input = ' key-a \n\nkey-b\nkey-a\n  key-c  \n';
    const result = parseCredentialLines(input);

    assert.deepEqual(result.keys, [
        { enabled: true, channel_key: 'key-a', remark: '' },
        { enabled: true, channel_key: 'key-b', remark: '' },
        { enabled: true, channel_key: 'key-c', remark: '' },
    ]);
    assert.equal(result.validCount, 3);
    assert.equal(result.duplicateCount, 1);
    assert.equal(result.blankCount, 1);
});

test('bulk credential parser handles a 100-key NVIDIA-sized pool without dropping keys', async () => {
    const { parseCredentialLines } = await loadImportHelpers();
    const input = Array.from({ length: 100 }, (_, index) => `nv-key-${index + 1}`).join('\n');
    const result = parseCredentialLines(input);

    assert.equal(result.keys.length, 100);
    assert.equal(result.validCount, 100);
    assert.equal(result.duplicateCount, 0);
    assert.equal(result.blankCount, 0);
    assert.equal(result.keys[0].channel_key, 'nv-key-1');
    assert.equal(result.keys[99].channel_key, 'nv-key-100');
});

test('large credential pool hint depends on local Channel concurrency, not a fake quota multiplier', async () => {
    const { shouldShowLargeCredentialPoolHint } = await loadImportHelpers();

    assert.equal(shouldShowLargeCredentialPoolHint(100, 3), true);
    assert.equal(shouldShowLargeCredentialPoolHint(100, 20), false);
    assert.equal(shouldShowLargeCredentialPoolHint(3, 3), false);
});

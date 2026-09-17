import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function read(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

async function readJSON(relativePath) {
    return JSON.parse(await read(relativePath));
}

test('provider preset and bulk-key create helpers use next-intl instead of hardcoded English copy', async () => {
    const picker = await read('../src/components/modules/channel/ProviderPresetPicker.tsx');
    const bulkImport = await read('../src/components/modules/channel/KeyBulkImport.tsx');

    assert.match(picker, /useTranslations\('channel\.create\.providerPreset'\)/);
    assert.match(bulkImport, /useTranslations\('channel\.create\.bulkKeys'\)/);
    assert.doesNotMatch(picker, />Provider preset</);
    assert.doesNotMatch(bulkImport, />Bulk import API keys</);
});

test('provider preset create copy exists in all supported locales', async () => {
    for (const locale of ['en', 'zh_hans', 'zh_hant']) {
        const messages = await readJSON(`../public/locale/${locale}.json`);
        const providerPreset = messages.channel?.create?.providerPreset;
        const bulkKeys = messages.channel?.create?.bulkKeys;

        assert.equal(typeof providerPreset?.label, 'string', `${locale}: providerPreset.label missing`);
        assert.equal(typeof providerPreset?.placeholder, 'string', `${locale}: providerPreset.placeholder missing`);
        assert.equal(typeof providerPreset?.hint, 'string', `${locale}: providerPreset.hint missing`);
        assert.equal(typeof bulkKeys?.title, 'string', `${locale}: bulkKeys.title missing`);
        assert.equal(typeof bulkKeys?.description, 'string', `${locale}: bulkKeys.description missing`);
        assert.equal(typeof bulkKeys?.import, 'string', `${locale}: bulkKeys.import missing`);
        assert.equal(typeof bulkKeys?.summary, 'string', `${locale}: bulkKeys.summary missing`);
        assert.equal(typeof bulkKeys?.concurrencyHint, 'string', `${locale}: bulkKeys.concurrencyHint missing`);
    }
});

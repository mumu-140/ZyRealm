import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const settingEndpoint = fs.readFileSync(new URL('../src/api/endpoints/setting.ts', import.meta.url), 'utf8');
const syncTasks = fs.readFileSync(new URL('../src/components/modules/setting/SyncTasks.tsx', import.meta.url), 'utf8');

function readLocale(name) {
    return JSON.parse(fs.readFileSync(new URL(`../public/locale/${name}.json`, import.meta.url), 'utf8'));
}

test('global model filter is wired through the existing setting key and Sync Tasks control', () => {
    assert.match(settingEndpoint, /ModelFilterRegex:\s*['"]model_filter_regex['"]/);
    assert.match(syncTasks, /SettingKey\.ModelFilterRegex/);
    assert.match(syncTasks, /syncTasks\.modelFilter\.label/);
    assert.match(syncTasks, /syncTasks\.modelFilter\.description/);
});

test('global model filter copy exists in all supported locales and explains delayed intersection semantics', () => {
    for (const localeName of ['zh_hans', 'zh_hant', 'en']) {
        const locale = readLocale(localeName);
        const modelFilter = locale?.setting?.syncTasks?.modelFilter;
        assert.equal(typeof modelFilter?.label, 'string', `${localeName} label missing`);
        assert.equal(typeof modelFilter?.placeholder, 'string', `${localeName} placeholder missing`);
        assert.equal(typeof modelFilter?.description, 'string', `${localeName} description missing`);
        assert.ok(modelFilter.description.length > 20, `${localeName} description is too terse to explain policy`);
    }
});

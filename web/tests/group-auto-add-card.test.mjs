import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('group card exposes a compact persisted auto-add quick action', async () => {
    const card = await source('../src/components/modules/group/Card.tsx');

    assert.match(card, /useGroupAutoAdd/);
    assert.match(card, /const\s+autoAdd\s*=\s*useGroupAutoAdd\(\)/);
    assert.match(card, /autoAdd\.mutate\(group\.id/);
    assert.match(card, /disabled=\{autoAdd\.isPending\s*\|\|\s*!group\.id\}/);
    assert.match(card, /result\.added\s*>\s*0/);
    assert.match(card, /toast\.success\(t\('autoAdd\.added'/);
    assert.match(card, /toast\.info\(t\('autoAdd\.noNew'/);
    assert.match(card, /toast\.error\(t\('autoAdd\.failed'/);
    assert.match(card, /<Sparkles\s+className="size-4"/);
});

test('group auto-add copy is localized in all supported locales and merged into group messages', async () => {
    const messages = await source('../src/provider/group-auto-add-messages.ts');
    const locale = await source('../src/provider/locale.tsx');

    for (const localeName of ['en', 'zh_hans', 'zh_hant']) {
        assert.match(messages, new RegExp(`${localeName}:\\s*\\{[\\s\\S]*?autoAdd:`));
    }
    for (const key of ['action', 'added', 'noNew', 'failed']) {
        assert.match(messages, new RegExp(`${key}:\\s*['\"]`), `missing localized ${key} copy`);
    }
    assert.match(locale, /groupAutoAddMessages/);
    assert.match(locale, /group:\s*\{[\s\S]*?autoAdd:\s*groupAutoAddMessages\[locale\]\.autoAdd/);
});

import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('group page owns the shared model-channel subscription and cards consume its index', async () => {
    const groupPage = await source('../src/components/modules/group/index.tsx');
    const card = await source('../src/components/modules/group/Card.tsx');

    assert.match(groupPage, /useModelChannelList\(\)/, 'Group page must subscribe to model-channel data once');
    assert.doesNotMatch(card, /useModelChannelList\(\)/, 'GroupCard must not create its own model-channel query observer');
    assert.match(card, /modelChannelByKey/, 'GroupCard must receive/use the shared model-channel index');
});

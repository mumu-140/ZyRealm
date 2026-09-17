import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('group member drag uses the supported clone path outside virtualized ancestors', async () => {
    const itemList = await source('../src/components/modules/group/ItemList.tsx');

    assert.match(itemList, /renderClone=/, 'member DnD must render the active item through Droppable.renderClone');
    assert.match(itemList, /getContainerForClone=/, 'member DnD must explicitly choose the clone container');
    assert.match(itemList, /document\.body/, 'the drag clone must be reparented to document.body');
});

test('group grid opts into a fixed-height transform virtual layout', async () => {
    const groupPage = await source('../src/components/modules/group/index.tsx');

    assert.match(groupPage, /GROUP_CARD_HEIGHT/, 'group grid must share the fixed card height constant');
    assert.match(groupPage, /measureRows=\{false\}/, 'group rows must not be dynamically remeasured');
    assert.match(groupPage, /positionMode="transform"/, 'group rows must use compositor-friendly transform positioning');
});

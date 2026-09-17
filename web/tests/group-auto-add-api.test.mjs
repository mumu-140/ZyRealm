import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('group auto-add is exposed as a dedicated persisted POST action', async () => {
    const handler = await source('../../internal/server/handlers/group.go');

    assert.match(
        handler,
        /NewRoute\("\/:id\/auto-add",\s*http\.MethodPost\)[\s\S]*?Handle\(autoAddGroup\)/,
        'group router must expose POST /:id/auto-add',
    );
    assert.match(handler, /func\s+autoAddGroup\s*\(/, 'group handler must define the auto-add action');
    assert.match(handler, /op\.GroupAutoAdd\(idNum,\s*c\.Request\.Context\(\)\)/, 'handler must delegate candidate resolution and persistence to op.GroupAutoAdd');
});

test('frontend group API exposes auto-add and refreshes persisted group state', async () => {
    const api = await source('../src/api/endpoints/group.ts');

    assert.match(api, /export\s+interface\s+GroupAutoAddResponse\s*\{[\s\S]*?matched:\s*number;[\s\S]*?added:\s*number;/);
    assert.match(api, /export\s+function\s+useGroupAutoAdd\s*\(/);
    assert.match(api, /\/api\/v1\/group\/\$\{id\}\/auto-add/);
    assert.match(
        api,
        /useGroupAutoAdd[\s\S]*?invalidateQueries\(\{\s*queryKey:\s*\['groups',\s*'list'\]\s*\}\)/,
        'auto-add mutation must refresh the canonical Group list cache',
    );
});

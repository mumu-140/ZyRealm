import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('live request API uses a dedicated one-second polling cache and interrupt endpoint', async () => {
    const api = await source('../src/api/endpoints/live-request.ts');

    assert.match(api, /\['live-requests'\]/, 'live requests must use a cache key separate from durable logs');
    assert.doesNotMatch(api, /\['logs'\]/, 'live requests must not share the RelayLog cache key');
    assert.match(api, /\/api\/v1\/live-request\/list/);
    assert.match(api, /refetchInterval:\s*1000/);
    assert.match(api, /\/api\/v1\/live-request\/\$\{encodeURIComponent\(requestID\)\}\/interrupt/);
});

test('logs page mounts a separate Active Requests surface above durable log history', async () => {
    const logPage = await source('../src/components/modules/log/index.tsx');
    const activeRequests = await source('../src/components/modules/log/ActiveRequests.tsx');

    assert.match(logPage, /import\s+\{\s*ActiveRequests\s*\}\s+from\s+'\.\/ActiveRequests'/);
    const activeIndex = logPage.indexOf('<ActiveRequests');
    const durableIndex = logPage.indexOf('<VirtualizedGrid');
    assert.ok(activeIndex >= 0, 'logs page must render ActiveRequests');
    assert.ok(durableIndex >= 0, 'logs page must keep the durable RelayLog grid');
    assert.ok(activeIndex < durableIndex, 'ActiveRequests must appear above durable log history');

    assert.match(activeRequests, /useLiveRequests/);
    assert.match(activeRequests, /useInterruptLiveRequest/);
    assert.match(activeRequests, /interrupt_requested/);
    assert.doesNotMatch(activeRequests, /request_body|authorization|api[_-]?key\b/i, 'live UI must not expose request bodies or credentials');
});

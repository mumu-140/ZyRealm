import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('routing inspector API is a historical read-only query', async () => {
    const api = await source('../src/api/endpoints/routing-inspector.ts');

    assert.match(api, /\['routing-explanation',\s*logID\]/);
    assert.match(api, /\/api\/v1\/log\/\$\{encodeURIComponent\(String\(logID\)\)\}\/routing/);
    assert.match(api, /useQuery/);
    assert.doesNotMatch(api, /useMutation|apiClient\.(post|put|patch|delete)/, 'inspector must not expose a mutation path');
});

test('logs page mounts a dedicated routing inspector without replacing existing log detail', async () => {
    const logPage = await source('../src/components/modules/log/index.tsx');
    const inspector = await source('../src/components/modules/log/RoutingInspector.tsx');

    assert.match(logPage, /import\s+\{\s*RoutingInspector\s*\}\s+from\s+'\.\/RoutingInspector'/);
    const activeIndex = logPage.indexOf('<ActiveRequests');
    const inspectorIndex = logPage.indexOf('<RoutingInspector');
    const durableIndex = logPage.indexOf('<VirtualizedGrid');
    const detailIndex = logPage.indexOf('<LogDetailModal');

    assert.ok(activeIndex >= 0, 'logs page must keep ActiveRequests');
    assert.ok(inspectorIndex > activeIndex, 'RoutingInspector must be a separate surface below ActiveRequests');
    assert.ok(durableIndex > inspectorIndex, 'durable log history must remain below RoutingInspector');
    assert.ok(detailIndex >= 0, 'existing LogDetailModal must remain available');

    assert.match(inspector, /Historical routing explanation/);
    assert.match(inspector, /read-only/i);
    assert.match(inspector, /final_route/);
    assert.match(inspector, /decisions/);
    assert.match(inspector, /attempts/);
    assert.doesNotMatch(
        inspector,
        /request_content|response_content|authorization|api[_-]?key\b|\.msg\b/i,
        'routing inspector must not reference raw payloads, credentials, or free-form attempt messages',
    );
});

import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('ActiveRequests is collapsible to prevent vertical layout occlusion', async () => {
    const activeRequests = await source('../src/components/modules/log/ActiveRequests.tsx');

    assert.match(activeRequests, /const\s*\[expanded,\s*setExpanded\]\s*=\s*useState\(true\)/, 'ActiveRequests must track expanded state');
    assert.match(activeRequests, /aria-expanded=\{expanded\}/, 'ActiveRequests header must expose accessible expanded state');
    assert.match(activeRequests, /ChevronDown/, 'ActiveRequests must render chevron toggle');
    assert.match(activeRequests, /setExpanded\(/, 'ActiveRequests must support toggling expanded state');
    assert.match(activeRequests, /\{expanded\s*\?\s*\(/, 'ActiveRequests must conditionally render list when expanded');
});

test('LogDetailModal embeds a collapsible LogDetailRoutingInspector on the right', async () => {
    const item = await source('../src/components/modules/log/Item.tsx');
    const inspector = await source('../src/components/modules/log/RoutingInspector.tsx');

    assert.match(item, /import\s+\{[^}]*LogDetailRoutingInspector[^}]*\}\s+from\s+'\.\/RoutingInspector'/);
    assert.match(item, /<LogDetailRoutingInspector\s+logId=\{displayLog\.id\}\s*\/>/);

    assert.match(inspector, /export\s+function\s+LogDetailRoutingInspector/);
    assert.match(inspector, /Historical routing explanation\s*·\s*read-only/);
    assert.match(inspector, /useRoutingExplanation\(expanded\s*\?\s*logId\s*:\s*null\)/, 'LogDetailRoutingInspector must not query when collapsed');
    assert.match(inspector, /completenessLabel/);
    assert.doesNotMatch(
        inspector,
        /request_content|response_content|authorization|api[_-]?key\b|\.msg\b/i,
        'LogDetailRoutingInspector must remain pure routing metadata without payloads or credentials',
    );
});

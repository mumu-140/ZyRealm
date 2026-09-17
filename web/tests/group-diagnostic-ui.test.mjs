import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('group cards expose health as an on-demand diagnostic action', async () => {
    const card = await source('../src/components/modules/group/Card.tsx');
    const health = await source('../src/components/modules/group/health.tsx');

    assert.match(card, /GroupDiagnosticAction/, 'Group card must expose the compact diagnostic action');
    assert.doesNotMatch(card, /<GroupHealthBadge/, 'Group card must not mount the old always-visible health badge');
    assert.doesNotMatch(health, /useGroupHealthList\(\)/, 'per-card health UI must not subscribe to the global polling list');
    assert.match(health, /useGroupHealth\(open \? groupId : null\)/, 'detail health query must only be enabled while the dialog is open');
    assert.doesNotMatch(health, /probeMode:\s*['"]full['"]/, 'primary diagnostic UI must not offer Full probe mode');
    assert.doesNotMatch(health, /runFull/, 'primary diagnostic UI must not expose Run Full');
});

test('diagnostic UI uses localized provider-risk and routing-score warnings', async () => {
    const health = await source('../src/components/modules/group/health.tsx');
    const messages = await source('../src/provider/group-diagnostic-messages.ts');
    const locale = await source('../src/provider/locale.tsx');

    assert.match(health, /t\(['"]warning\.realRequest['"]\)/);
    assert.match(health, /t\(['"]warning\.quota['"]\)/);
    assert.match(health, /t\(['"]warning\.routing['"]\)/);
    assert.match(messages, /en:/);
    assert.match(messages, /zh_hans:/);
    assert.match(messages, /zh_hant:/);
    assert.match(locale, /groupDiagnosticMessages/);
    assert.match(locale, /health:\s*\{/);
});

import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('retired circuit settings are absent from the active web settings surface', async () => {
    const [api, reliability, en, zhHans, zhHant] = await Promise.all([
        source('../src/api/endpoints/setting.ts'),
        source('../src/components/modules/setting/Reliability.tsx'),
        source('../public/locale/en.json'),
        source('../public/locale/zh_hans.json'),
        source('../public/locale/zh_hant.json'),
    ]);

    for (const [name, content] of [
        ['setting API', api],
        ['Reliability UI', reliability],
    ]) {
        assert.doesNotMatch(content, /CircuitBreaker|circuitBreaker|circuit_breaker_/,
            `${name} must not expose retired breaker configuration`);
    }

    for (const [name, content] of [
        ['en locale', en],
        ['zh_hans locale', zhHans],
        ['zh_hant locale', zhHant],
    ]) {
        assert.doesNotMatch(content, /"circuitBreaker"\s*:/,
            `${name} must not carry active breaker copy`);
    }
});

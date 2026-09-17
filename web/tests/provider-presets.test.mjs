import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function sourceOrEmpty(relativePath) {
    try {
        return await readFile(new URL(relativePath, import.meta.url), 'utf8');
    } catch (error) {
        if (error && typeof error === 'object' && 'code' in error && error.code === 'ENOENT') return '';
        throw error;
    }
}

test('provider preset registry defines NVIDIA and AMD as OpenAI-compatible dynamic-discovery templates', async () => {
    const registry = await sourceOrEmpty('../src/components/modules/channel/provider-presets.ts');

    assert.match(registry, /id:\s*'nvidia'/);
    assert.match(registry, /https:\/\/integrate\.api\.nvidia\.com\/v1/);
    assert.match(registry, /id:\s*'amd-radeon-cloud'/);
    assert.match(registry, /https:\/\/developer\.amd\.com\.cn\/radeon\/api\/v1/);
    assert.match(registry, /ChannelType\.OpenAIChat/);
    assert.match(registry, /modelDiscovery:\s*'openai'/);
    assert.doesNotMatch(registry, /\bmodels\s*:/, 'presets must not ship static model lists');
});

test('provider preset registry keeps a generic editable OpenAI-compatible escape hatch', async () => {
    const registry = await sourceOrEmpty('../src/components/modules/channel/provider-presets.ts');

    assert.match(registry, /id:\s*'custom-openai'/);
    assert.match(registry, /baseUrl:\s*''/);
});

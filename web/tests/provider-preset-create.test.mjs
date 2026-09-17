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

test('channel create exposes provider preset picker and applies presets to existing form data', async () => {
    const create = await sourceOrEmpty('../src/components/modules/channel/Create.tsx');
    const picker = await sourceOrEmpty('../src/components/modules/channel/ProviderPresetPicker.tsx');

    assert.match(create, /ProviderPresetPicker/);
    assert.match(create, /getProviderPreset/);
    assert.match(create, /base_urls:\s*\[\{\s*url:\s*preset\.baseUrl,\s*delay:\s*0\s*\}\]/s);
    assert.match(create, /type:\s*preset\.type/);
    assert.match(create, /model:\s*''/);
    assert.match(create, /custom_model:\s*''/);
    assert.match(create, /keys:\s*\[\{\s*enabled:\s*true,\s*channel_key:\s*'',\s*remark:\s*''\s*\}\]/s,
        'switching provider presets must clear previously entered credentials');

    assert.match(picker, /PROVIDER_PRESETS/);
    assert.match(picker, /onSelect/);
});

test('preset picker is create-only and leaves the existing ChannelForm submission path intact', async () => {
    const create = await sourceOrEmpty('../src/components/modules/channel/Create.tsx');

    assert.match(create, /<ChannelForm/);
    assert.match(create, /createChannel\.mutate\(/);
    assert.match(create, /<ProviderPresetPicker/);
});

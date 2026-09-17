import { ChannelType } from '@/api/endpoints/channel';

export type ProviderPresetID =
    | 'nvidia'
    | 'amd-radeon-cloud'
    | 'openrouter'
    | 'groq'
    | 'siliconflow'
    | 'custom-openai';

export type ProviderPreset = {
    id: ProviderPresetID;
    name: string;
    type: ChannelType;
    baseUrl: string;
    apiKeyRequired: boolean;
    modelDiscovery: 'openai' | 'manual';
};

export const PROVIDER_PRESETS: readonly ProviderPreset[] = [
    {
        id: 'nvidia',
        name: 'NVIDIA NIM',
        type: ChannelType.OpenAIChat,
        baseUrl: 'https://integrate.api.nvidia.com/v1',
        apiKeyRequired: true,
        modelDiscovery: 'openai',
    },
    {
        id: 'amd-radeon-cloud',
        name: 'AMD Radeon Cloud',
        type: ChannelType.OpenAIChat,
        baseUrl: 'https://developer.amd.com.cn/radeon/api/v1',
        apiKeyRequired: true,
        modelDiscovery: 'openai',
    },
    {
        id: 'openrouter',
        name: 'OpenRouter',
        type: ChannelType.OpenAIChat,
        baseUrl: 'https://openrouter.ai/api/v1',
        apiKeyRequired: true,
        modelDiscovery: 'openai',
    },
    {
        id: 'groq',
        name: 'Groq',
        type: ChannelType.OpenAIChat,
        baseUrl: 'https://api.groq.com/openai/v1',
        apiKeyRequired: true,
        modelDiscovery: 'openai',
    },
    {
        id: 'siliconflow',
        name: 'SiliconFlow',
        type: ChannelType.OpenAIChat,
        baseUrl: 'https://api.siliconflow.cn/v1',
        apiKeyRequired: true,
        modelDiscovery: 'openai',
    },
    {
        id: 'custom-openai',
        name: 'Custom OpenAI-compatible',
        type: ChannelType.OpenAIChat,
        baseUrl: '',
        apiKeyRequired: false,
        modelDiscovery: 'openai',
    },
];

export function getProviderPreset(id: ProviderPresetID): ProviderPreset {
    const preset = PROVIDER_PRESETS.find((item) => item.id === id);
    if (!preset) {
        throw new Error(`Unknown provider preset: ${id}`);
    }
    return preset;
}

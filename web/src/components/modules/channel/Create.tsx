import { useState } from 'react';
import {
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { useCreateChannel, ChannelType, AutoGroupType } from '@/api/endpoints/channel';
import { useTranslations } from 'next-intl';
import { toast } from '@/components/common/Toast';
import { ChannelForm, type ChannelFormData } from './Form';
import { ProviderPresetPicker } from './ProviderPresetPicker';
import { KeyBulkImport } from './KeyBulkImport';
import { getProviderPreset, type ProviderPresetID } from './provider-presets';
import { mergeCredentialKeys, type ImportedCredentialKey } from './key-import';

function createDefaultFormData(): ChannelFormData {
    return {
        name: '',
        type: ChannelType.OpenAIChat,
        base_urls: [{ url: '', delay: 0 }],
        custom_header: [],
        ws_mode: 'inherit',
        proxy_mode: 'direct',
        proxy_config_id: null,
        param_override: '',
        keys: [{ enabled: true, channel_key: '', remark: '' }],
        model: '',
        custom_model: '',
        auto_sync: false,
        auto_group: AutoGroupType.None,
        enabled: true,
        max_concurrency: 3,
        max_rpm: 0,
        match_regex: '',
    };
}

export function CreateDialogContent() {
    const { setIsOpen } = useMorphingDialog();
    const createChannel = useCreateChannel();
    const [formData, setFormData] = useState<ChannelFormData>(createDefaultFormData);
    const [selectedPreset, setSelectedPreset] = useState<ProviderPresetID>();
    const t = useTranslations('channel.create');
    const tProxy = useTranslations('proxyPool');

    const handlePresetSelect = (id: ProviderPresetID) => {
        const preset = getProviderPreset(id);
        setSelectedPreset(id);
        setFormData((current) => ({
            ...current,
            name: preset.name,
            type: preset.type,
            base_urls: [{ url: preset.baseUrl, delay: 0 }],
            keys: [{ enabled: true, channel_key: '', remark: '' }],
            model: '',
            custom_model: '',
        }));
    };

    const handleBulkKeyImport = (keys: ImportedCredentialKey[]) => {
        setFormData((current) => ({
            ...current,
            keys: mergeCredentialKeys(current.keys, keys),
        }));
    };

    const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const normalizedBaseUrls = (formData.base_urls ?? []).filter((u) => u.url.trim()).map((u) => ({
            url: u.url.trim(),
            delay: Number(u.delay || 0),
        }));
        const normalizedKeys = formData.keys
            .filter((k) => k.channel_key.trim())
            .map((k) => ({ enabled: k.enabled, channel_key: k.channel_key, remark: k.remark ?? '' }));
        const normalizedHeaders = (formData.custom_header ?? [])
            .map((h) => ({ header_key: h.header_key.trim(), header_value: h.header_value }))
            .filter((h) => h.header_key && h.header_value !== '');

        const paramOverride = formData.param_override.trim();
        if (formData.proxy_mode === 'pool' && !formData.proxy_config_id) {
            toast.error(tProxy('selectRequired'));
            return;
        }
        createChannel.mutate(
            {
                name: formData.name,
                type: formData.type,
                enabled: formData.enabled,
                max_concurrency: formData.max_concurrency,
                max_rpm: formData.max_rpm,
                base_urls: normalizedBaseUrls,
                keys: normalizedKeys,
                model: formData.model,
                custom_model: formData.custom_model,
                proxy_mode: formData.proxy_mode,
                proxy_config_id: formData.proxy_mode === 'pool' ? formData.proxy_config_id : null,
                auto_sync: formData.auto_sync,
                auto_group: formData.auto_group,
                custom_header: normalizedHeaders,
                ws_mode: formData.ws_mode,
                param_override: paramOverride,
                match_regex: formData.match_regex.trim(),
            },
            {
                onSuccess: () => {
                    setFormData(createDefaultFormData());
                    setSelectedPreset(undefined);
                    setIsOpen(false);
                }
            });
    };

    const credentialCount = formData.keys.filter((key) => key.channel_key.trim()).length;

    return (
        <div className="w-screen max-w-full md:max-w-xl h-full min-h-0 flex flex-col">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-6 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground">{t('dialogTitle')}</h2>
                    <MorphingDialogClose
                        className="relative right-0 top-0"
                        variants={{
                            initial: { opacity: 0, scale: 0.8 },
                            animate: { opacity: 1, scale: 1 },
                            exit: { opacity: 0, scale: 0.8 }
                        }}
                    />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription disableLayoutAnimation className="flex-1 min-h-0 overflow-auto">
                <div className="space-y-4">
                    <ProviderPresetPicker value={selectedPreset} onSelect={handlePresetSelect} />
                    <KeyBulkImport
                        credentialCount={credentialCount}
                        maxConcurrency={formData.max_concurrency}
                        onImport={handleBulkKeyImport}
                    />
                    <ChannelForm
                        formData={formData}
                        onFormDataChange={setFormData}
                        onSubmit={handleSubmit}
                        isPending={createChannel.isPending}
                        submitText={t('submit')}
                        pendingText={t('submitting')}
                        idPrefix="new-channel"
                    />
                </div>
            </MorphingDialogDescription>
        </div>
    );
}

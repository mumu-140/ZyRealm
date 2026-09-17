import { useTranslations } from 'next-intl';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { PROVIDER_PRESETS, type ProviderPresetID } from './provider-presets';

interface ProviderPresetPickerProps {
    value?: ProviderPresetID;
    onSelect: (id: ProviderPresetID) => void;
}

export function ProviderPresetPicker({ value, onSelect }: ProviderPresetPickerProps) {
    const t = useTranslations('channel.create.providerPreset');

    return (
        <div className="space-y-2">
            <label className="text-sm font-medium text-card-foreground">{t('label')}</label>
            <Select
                value={value}
                onValueChange={(nextValue) => onSelect(nextValue as ProviderPresetID)}
            >
                <SelectTrigger className="w-full rounded-xl">
                    <SelectValue placeholder={t('placeholder')} />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    {PROVIDER_PRESETS.map((preset) => (
                        <SelectItem key={preset.id} value={preset.id} className="rounded-xl">
                            {preset.name}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t('hint')}</p>
        </div>
    );
}

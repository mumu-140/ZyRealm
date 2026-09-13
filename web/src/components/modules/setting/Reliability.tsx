'use client';

import { useTranslations } from 'next-intl';
import { Gauge, Hash, HeartPulse, ShieldCheck, Timer, TimerOff, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

const RELAY_MAX_WIRE_ATTEMPTS_KEY = 'relay_max_wire_attempts';

// min/max 与后端 model.Setting.Validate() 的边界保持一致，前端先行约束整数范围。
const OUTLIER_FIELDS: { key: string; labelKey: string; min: number; max?: number }[] = [
    { key: SettingKey.OutlierRetireInterval, labelKey: 'interval', min: 1 },
    { key: SettingKey.OutlierFailRatePct, labelKey: 'failRate', min: 1, max: 100 },
    { key: SettingKey.OutlierMinSamples, labelKey: 'minSamples', min: 1 },
    { key: SettingKey.OutlierConsecFails, labelKey: 'consecFails', min: 1 },
    { key: SettingKey.OutlierWindowMinutes, labelKey: 'windowMinutes', min: 1 },
    { key: SettingKey.OutlierWindowCapacity, labelKey: 'windowCapacity', min: 1, max: 20 },
    { key: SettingKey.OutlierRecoverStreak, labelKey: 'recoverStreak', min: 1 },
    { key: SettingKey.OutlierReapMinutes, labelKey: 'reapMinutes', min: 1 },
    { key: SettingKey.OutlierCFRecoverMinutes, labelKey: 'cfRecoverMinutes', min: 1 },
];

function NumberFieldRow({ settingKey, label, placeholder, tooltip, icon, min, max }: {
    settingKey: string;
    label: string;
    placeholder: string;
    tooltip?: React.ReactNode;
    icon?: LucideIcon;
    min?: number;
    max?: number;
}) {
    const field = useSettingField(settingKey);
    return (
        <SettingRow icon={icon} label={label} tooltip={tooltip}>
            <Input
                type="number"
                step={1}
                min={min}
                max={max}
                value={field.value}
                onChange={(e) => field.setValue(e.target.value)}
                onBlur={field.save}
                placeholder={placeholder}
                className="w-48 rounded-xl"
            />
        </SettingRow>
    );
}

export function SettingReliability() {
    const t = useTranslations('setting');
    const outlier = useSettingToggle(SettingKey.OutlierRetireEnabled);
    const groupHealth = useSettingToggle(SettingKey.GroupHealthEnabled);

    return (
        <SettingCard icon={ShieldCheck} title={t('reliability.title')}>
            {/* 请求级路由预算 */}
            <SettingSection
                title="请求级路由预算"
                tooltip="限制单个请求真正发往上游的总尝试次数，包括同渠道重试、凭据轮换和协议/Provider failover。Provider 数量上限仍为 4；未知上游结果的跨 Provider 重放仍最多 1 次。"
            />
            <NumberFieldRow
                settingKey={RELAY_MAX_WIRE_ATTEMPTS_KEY}
                label="最大上游尝试次数"
                placeholder="20"
                tooltip="允许范围 1–20，默认 20。达到预算后不再发起新的上游调用；服务端始终硬限制为最多 20。"
                icon={Gauge}
                min={1}
                max={20}
            />

            {/* 分组健康检查 */}
            <SettingRow icon={HeartPulse} label={t('groupHealth.label')} tooltip={t('groupHealth.description')}>
                <Switch checked={groupHealth.enabled} onCheckedChange={groupHealth.toggle} />
            </SettingRow>

            {/* 熔断器 */}
            <SettingSection title={t('circuitBreaker.title')} tooltip={t('circuitBreaker.hint')} />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerThreshold}
                label={t('circuitBreaker.threshold.label')}
                placeholder={t('circuitBreaker.threshold.placeholder')}
                icon={Hash}
            />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerCooldown}
                label={t('circuitBreaker.cooldown.label')}
                placeholder={t('circuitBreaker.cooldown.placeholder')}
                icon={Timer}
            />
            <NumberFieldRow
                settingKey={SettingKey.CircuitBreakerMaxCooldown}
                label={t('circuitBreaker.maxCooldown.label')}
                placeholder={t('circuitBreaker.maxCooldown.placeholder')}
                icon={TimerOff}
            />

            {/* 被动离群退役 */}
            <SettingSection title={t('outlierRetirement.title')} tooltip={t('outlierRetirement.hint')} />
            <SettingRow label={t('outlierRetirement.enabled.label')}>
                <Switch checked={outlier.enabled} onCheckedChange={outlier.toggle} />
            </SettingRow>
            {outlier.enabled && OUTLIER_FIELDS.map((f) => (
                <NumberFieldRow
                    key={f.key}
                    settingKey={f.key}
                    label={t(`outlierRetirement.${f.labelKey}.label`)}
                    placeholder={t(`outlierRetirement.${f.labelKey}.placeholder`)}
                    tooltip={t(`outlierRetirement.${f.labelKey}.description`)}
                    min={f.min}
                    max={f.max}
                />
            ))}
        </SettingCard>
    );
}

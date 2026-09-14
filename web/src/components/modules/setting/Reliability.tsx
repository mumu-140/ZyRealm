'use client';

import { useTranslations } from 'next-intl';
import { Gauge, Hash, HeartPulse, Network, ShieldCheck, Timer, TimerOff, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingKey } from '@/api/endpoints/setting';
import { useSettingStore, type Locale } from '@/stores/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

const RELAY_MAX_PROVIDER_ATTEMPTS_KEY = 'relay_max_provider_attempts';
const RELAY_MAX_WIRE_ATTEMPTS_KEY = 'relay_max_wire_attempts';

const ROUTING_BUDGET_COPY: Record<Locale, {
    title: string;
    hint: string;
    providerLabel: string;
    providerHint: string;
    wireLabel: string;
    wireHint: string;
}> = {
    zh_hans: {
        title: '请求级路由预算',
        hint: '两个预算独立生效，并由你按上游规模设置。不同上游渠道数控制单请求最多进入多少个不同 Channel；真实发送次数控制实际发往上游的总 wire attempt。同渠道的凭据轮换和协议重试只增加真实发送次数，不重复增加渠道数。未知执行结果的跨 Provider 重放仍最多 1 次。',
        providerLabel: '最大不同上游渠道数',
        providerHint: '必须为正整数，无代码级最大值。默认 20。第 N+1 个新渠道只会在达到你设置的此预算后才被阻止。',
        wireLabel: '最大真实上游发送次数',
        wireHint: '必须为正整数，无代码级最大值。默认 20。每次真正发送到上游都会计数，包括同渠道的凭据轮换和协议重试。',
    },
    zh_hant: {
        title: '請求級路由預算',
        hint: '兩個預算獨立生效，並由你按上游規模設定。不同上游渠道數控制單請求最多進入多少個不同 Channel；真實發送次數控制實際送往上游的總 wire attempt。同渠道的憑據輪換和協議重試只增加真實發送次數，不重複增加渠道數。未知執行結果的跨 Provider 重放仍最多 1 次。',
        providerLabel: '最大不同上游渠道數',
        providerHint: '必須為正整數，無程式碼級最大值。預設 20。第 N+1 個新渠道只會在達到你設定的此預算後才被阻止。',
        wireLabel: '最大真實上游發送次數',
        wireHint: '必須為正整數，無程式碼級最大值。預設 20。每次真正送到上游都會計數，包括同渠道的憑據輪換和協議重試。',
    },
    en: {
        title: 'Request routing budgets',
        hint: 'The two budgets are independent and administrator-defined. Distinct upstream channels limits how many different Channels one request may enter; real upstream sends limits total wire attempts. Credential rotation and protocol retries inside an already-seen Channel consume wire attempts without consuming another channel slot. Unknown-outcome cross-provider replay remains limited to 1.',
        providerLabel: 'Maximum distinct upstream channels',
        providerHint: 'Must be a positive integer. There is no application-level maximum. Default: 20. A new Channel is blocked only after this configured budget is reached.',
        wireLabel: 'Maximum real upstream sends',
        wireHint: 'Must be a positive integer. There is no application-level maximum. Default: 20. Every real upstream send counts, including credential rotation and protocol retries within a Channel.',
    },
};

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
    const locale = useSettingStore((state) => state.locale);
    const routingBudget = ROUTING_BUDGET_COPY[locale];
    const outlier = useSettingToggle(SettingKey.OutlierRetireEnabled);
    const groupHealth = useSettingToggle(SettingKey.GroupHealthEnabled);

    return (
        <SettingCard icon={ShieldCheck} title={t('reliability.title')}>
            {/* 请求级路由预算 */}
            <SettingSection title={routingBudget.title} tooltip={routingBudget.hint} />
            <NumberFieldRow
                settingKey={RELAY_MAX_PROVIDER_ATTEMPTS_KEY}
                label={routingBudget.providerLabel}
                placeholder="20"
                tooltip={routingBudget.providerHint}
                icon={Network}
                min={1}
            />
            <NumberFieldRow
                settingKey={RELAY_MAX_WIRE_ATTEMPTS_KEY}
                label={routingBudget.wireLabel}
                placeholder="20"
                tooltip={routingBudget.wireHint}
                icon={Gauge}
                min={1}
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

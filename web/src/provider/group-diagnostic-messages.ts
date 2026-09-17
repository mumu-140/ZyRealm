import type { Locale } from '@/stores/setting';

export const groupDiagnosticMessages: Record<Locale, { health: Record<string, unknown> }> = {
    en: {
        health: {
            diagnosticAction: 'Run diagnostic',
            diagnosticTitle: 'Group diagnostic',
            diagnosticDescription: 'Inspect the latest active probe result for this group.',
            runDiagnostic: 'Run diagnostic',
            warning: {
                realRequest: 'This sends a real synthetic request to the provider.',
                quota: 'It may consume quota or cost and may be blocked by provider policy.',
                routing: 'This is diagnostic evidence only; it does not set the HealthFirst routing score.',
            },
        },
    },
    zh_hans: {
        health: {
            diagnosticAction: '运行诊断',
            diagnosticTitle: '分组诊断',
            diagnosticDescription: '查看该分组最近一次主动探测结果。',
            runDiagnostic: '运行诊断',
            warning: {
                realRequest: '该操作会向上游提供商发送一次真实的合成请求。',
                quota: '请求可能消耗额度或产生费用，也可能被提供商策略拦截。',
                routing: '该结果仅用于诊断，不会设置 HealthFirst 的路由健康分数。',
            },
        },
    },
    zh_hant: {
        health: {
            diagnosticAction: '執行診斷',
            diagnosticTitle: '分組診斷',
            diagnosticDescription: '查看該分組最近一次主動探測結果。',
            runDiagnostic: '執行診斷',
            warning: {
                realRequest: '此操作會向上游供應商送出一次真實的合成請求。',
                quota: '請求可能消耗額度或產生費用，也可能被供應商政策攔截。',
                routing: '此結果僅供診斷，不會設定 HealthFirst 的路由健康分數。',
            },
        },
    },
};

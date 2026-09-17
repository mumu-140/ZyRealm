export const channelCreateMessages = {
    en: {
        providerPreset: {
            label: 'Provider preset',
            placeholder: 'Choose a provider preset',
            hint: 'Presets only prefill the existing Channel form. Base URL and other settings remain editable.',
        },
        bulkKeys: {
            title: 'Bulk import API keys',
            description: 'One credential per line. Duplicates and blank lines are ignored.',
            import: 'Import',
            summary: '{valid} valid / {duplicates} duplicates / {blank} blank lines ignored',
            concurrencyHint: 'This Channel currently allows only {maxConcurrency} concurrent requests. A large key pool is still capped by the Channel concurrency limit; key count does not imply provider quota.',
        },
    },
    zh_hans: {
        providerPreset: {
            label: '供应商预设',
            placeholder: '选择供应商预设',
            hint: '预设仅用于预填现有渠道表单，Base URL 和其他设置仍可修改。',
        },
        bulkKeys: {
            title: '批量导入 API Key',
            description: '每行一个凭据；重复项和空行会自动忽略。',
            import: '导入',
            summary: '{valid} 个有效 / {duplicates} 个重复 / 忽略 {blank} 个空行',
            concurrencyHint: '当前渠道最多允许 {maxConcurrency} 个并发请求。即使导入大量 Key，仍受渠道并发上限约束；Key 数量不代表供应商配额。',
        },
    },
    zh_hant: {
        providerPreset: {
            label: '供應商預設',
            placeholder: '選擇供應商預設',
            hint: '預設僅用於預填現有供應源表單，Base URL 和其他設定仍可修改。',
        },
        bulkKeys: {
            title: '批次匯入 API Key',
            description: '每行一個憑證；重複項與空行會自動忽略。',
            import: '匯入',
            summary: '{valid} 個有效 / {duplicates} 個重複 / 忽略 {blank} 個空行',
            concurrencyHint: '目前供應源最多允許 {maxConcurrency} 個並發請求。即使匯入大量 Key，仍受供應源並發上限約束；Key 數量不代表供應商配額。',
        },
    },
} as const;

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import {
    parseCredentialLines,
    shouldShowLargeCredentialPoolHint,
    type ImportedCredentialKey,
} from './key-import';

interface KeyBulkImportProps {
    credentialCount: number;
    maxConcurrency: number;
    onImport: (keys: ImportedCredentialKey[]) => void;
}

export function KeyBulkImport({ credentialCount, maxConcurrency, onImport }: KeyBulkImportProps) {
    const [input, setInput] = useState('');
    const parsed = useMemo(() => parseCredentialLines(input), [input]);
    const showConcurrencyHint = shouldShowLargeCredentialPoolHint(credentialCount, maxConcurrency);
    const t = useTranslations('channel.create.bulkKeys');

    return (
        <div className="space-y-2 rounded-xl border border-border p-3">
            <div className="flex items-center justify-between gap-3">
                <div>
                    <div className="text-sm font-medium text-card-foreground">{t('title')}</div>
                    <p className="text-xs text-muted-foreground">{t('description')}</p>
                </div>
                <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={parsed.validCount === 0}
                    onClick={() => {
                        onImport(parsed.keys);
                        setInput('');
                    }}
                >
                    {t('import')}
                </Button>
            </div>
            <textarea
                value={input}
                onChange={(event) => setInput(event.target.value)}
                rows={4}
                autoComplete="off"
                spellCheck={false}
                placeholder="nvapi-..."
                className="w-full resize-y rounded-xl border border-input bg-background px-3 py-2 font-mono text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            {input.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                    {t('summary', {
                        valid: parsed.validCount,
                        duplicates: parsed.duplicateCount,
                        blank: parsed.blankCount,
                    })}
                </p>
            ) : null}
            {showConcurrencyHint ? (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                    {t('concurrencyHint', { maxConcurrency })}
                </p>
            ) : null}
        </div>
    );
}

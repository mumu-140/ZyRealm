export type ImportedCredentialKey = {
    enabled: true;
    channel_key: string;
    remark: string;
};

export type CredentialImportResult = {
    keys: ImportedCredentialKey[];
    validCount: number;
    duplicateCount: number;
    blankCount: number;
};

type ExistingCredentialKey = {
    enabled: boolean;
    channel_key: string;
    remark?: string;
};

export function parseCredentialLines(input: string): CredentialImportResult {
    const lines = input.split(/\r?\n/);
    if (lines.length > 1 && lines[lines.length - 1] === '') {
        lines.pop();
    }

    const seen = new Set<string>();
    const keys: ImportedCredentialKey[] = [];
    let duplicateCount = 0;
    let blankCount = 0;

    for (const line of lines) {
        const credential = line.trim();
        if (!credential) {
            blankCount += 1;
            continue;
        }
        if (seen.has(credential)) {
            duplicateCount += 1;
            continue;
        }
        seen.add(credential);
        keys.push({ enabled: true, channel_key: credential, remark: '' });
    }

    return {
        keys,
        validCount: keys.length,
        duplicateCount,
        blankCount,
    };
}

export function mergeCredentialKeys(
    existing: ExistingCredentialKey[],
    imported: ImportedCredentialKey[],
): ExistingCredentialKey[] {
    const merged: ExistingCredentialKey[] = [];
    const seen = new Set<string>();

    for (const key of [...existing, ...imported]) {
        const credential = key.channel_key.trim();
        if (!credential || seen.has(credential)) continue;
        seen.add(credential);
        merged.push({
            enabled: key.enabled,
            channel_key: credential,
            remark: key.remark ?? '',
        });
    }

    return merged.length > 0
        ? merged
        : [{ enabled: true, channel_key: '', remark: '' }];
}

export function shouldShowLargeCredentialPoolHint(
    credentialCount: number,
    maxConcurrency: number,
): boolean {
    return credentialCount >= 20 && maxConcurrency > 0 && maxConcurrency <= 3;
}

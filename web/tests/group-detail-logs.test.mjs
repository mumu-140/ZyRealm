import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

async function source(relativePath) {
    return readFile(new URL(relativePath, import.meta.url), 'utf8');
}

test('jump store supports log-detail and log-group targets routing to log view', async () => {
    const jump = await source('../src/stores/jump.ts');

    assert.match(jump, /export\s+type\s+LogJumpTarget\s*=/, 'jump store must declare LogJumpTarget type');
    assert.match(jump, /\{\s*kind:\s*'log-detail';\s*logId:\s*number\s*\}/, 'LogJumpTarget must support log-detail with logId');
    assert.match(jump, /\{\s*kind:\s*'log-group';\s*groupName:\s*string\s*\}/, 'LogJumpTarget must support log-group with groupName');
    assert.match(jump, /isLogJumpTarget/, 'jump store must export isLogJumpTarget guard');
    assert.match(jump, /case 'log-detail':\s*case 'log-group':\s*return 'log';/, 'getJumpTargetRoute must route log targets to log page');
});

test('log page consumes pendingJump to open log detail modal or apply group filter', async () => {
    const logIndex = await source('../src/components/modules/log/index.tsx');

    assert.match(logIndex, /import\s*\{[^}]*getLogDetail[^}]*\}\s*from\s*'@\/api\/endpoints\/log'/, 'log index must import getLogDetail');
    assert.match(logIndex, /import\s*\{[^}]*useJumpStore[^}]*\}\s*from\s*'@\/stores\/jump'/, 'log index must import useJumpStore');
    assert.match(logIndex, /pendingJump\.target\.kind\s*===\s*'log-detail'/, 'log index must handle log-detail pending jump');
    assert.match(logIndex, /setSelectedLog\(/, 'log index must set selectedLog to open detail modal');
    assert.match(logIndex, /clearPending\(/, 'log index must clear pending jump after consumption');
});

test('GroupLogsPanel component renders active requests and recent history with jump triggers', async () => {
    const panel = await source('../src/components/modules/group/GroupLogsPanel.tsx');

    assert.match(panel, /export\s+function\s+GroupLogsPanel/, 'GroupLogsPanel must be exported');
    assert.match(panel, /useLiveRequests\(\)/, 'GroupLogsPanel must query live requests for active list');
    assert.match(panel, /useLogPage\(/, 'GroupLogsPanel must query recent history logs');
    assert.match(panel, /useInterruptLiveRequest\(\)/, 'GroupLogsPanel must support interrupting active requests');
    assert.match(panel, /useJumpStore\.getState\(\)\.requestJump\(\{\s*kind:\s*'log-detail'/, 'clicking history item must jump to log-detail');
    assert.match(panel, /useJumpStore\.getState\(\)\.requestJump\(\{\s*kind:\s*'log-group'/, 'clicking view-all must jump to log-group');
    assert.match(panel, /matchesGroupName/, 'GroupLogsPanel must match requests by group name or regex');
});

test('GroupCard embeds GroupLogsPanel on the right side of detail block and displays active count badge', async () => {
    const card = await source('../src/components/modules/group/Card.tsx');

    assert.match(card, /import\s*\{[^}]*GroupLogsPanel[^}]*\}\s*from\s*'\.\/GroupLogsPanel'/, 'Card must import GroupLogsPanel');
    assert.match(card, /<GroupLogsPanel\s+group=\{group\}/, 'EditDialogContent must embed GroupLogsPanel');
    assert.match(card, /useLiveRequests\(\)/, 'GroupCard must query live requests to detect active state');
    assert.match(card, /activeCount/, 'GroupCard must compute activeCount');
    assert.match(card, /max-w-5xl\s+lg:max-w-7xl\s+xl:max-w-\[1440px\]/, 'MorphingDialogContent must be widened for side-by-side layout');
});

test('locales contain group.logs translations in zh_hans, en, and zh_hant', async () => {
    const zhHans = JSON.parse(await source('../public/locale/zh_hans.json'));
    const en = JSON.parse(await source('../public/locale/en.json'));
    const zhHant = JSON.parse(await source('../public/locale/zh_hant.json'));

    assert.ok(zhHans.group.logs, 'zh_hans must have group.logs');
    assert.equal(zhHans.group.logs.title, '运行日志');
    assert.equal(zhHans.group.logs.activeTitle, '活跃请求');

    assert.ok(en.group.logs, 'en must have group.logs');
    assert.equal(en.group.logs.title, 'Group Logs');
    assert.equal(en.group.logs.activeTitle, 'Active Requests');

    assert.ok(zhHant.group.logs, 'zh_hant must have group.logs');
    assert.equal(zhHant.group.logs.title, '運行日誌');
    assert.equal(zhHant.group.logs.activeTitle, '活躍請求');
});

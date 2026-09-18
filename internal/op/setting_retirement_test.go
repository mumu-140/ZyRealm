package op

import (
    "testing"

    dbpkg "github.com/bestruirui/octopus/internal/db"
    "github.com/bestruirui/octopus/internal/model"
)

func TestRetiredCircuitSettingsStayOutOfActiveRuntimeAndExports(t *testing.T) {
    ctx := setupBackupTestDB(t)
    settingCache.Clear()
    t.Cleanup(settingCache.Clear)

    retired := []model.Setting{
        {Key: model.SettingKeyCircuitBreakerThreshold, Value: "9"},
        {Key: model.SettingKeyCircuitBreakerCooldown, Value: "90"},
        {Key: model.SettingKeyCircuitBreakerMaxCooldown, Value: "900"},
    }
    for _, setting := range retired {
        if err := dbpkg.GetDB().Save(&setting).Error; err != nil {
            t.Fatalf("seed retired setting %s: %v", setting.Key, err)
        }
    }

    if err := settingRefreshCache(ctx); err != nil {
        t.Fatalf("settingRefreshCache failed: %v", err)
    }
    for _, setting := range retired {
        if _, err := SettingGetString(setting.Key); err == nil {
            t.Fatalf("retired setting %s must not enter the active cache", setting.Key)
        }
    }

    dump, err := DBExportAll(ctx, false, false)
    if err != nil {
        t.Fatalf("DBExportAll failed: %v", err)
    }
    for _, setting := range dump.Settings {
        if model.IsRetiredSettingKey(setting.Key) {
            t.Fatalf("retired setting %s must not be exported", setting.Key)
        }
    }
}

func TestRetiredCircuitSettingsFromBackupAreIgnored(t *testing.T) {
    ctx := setupBackupTestDB(t)
    dump := &model.DBDump{
        Version: 1,
        Settings: []model.Setting{
            {Key: model.SettingKeyCircuitBreakerThreshold, Value: "9"},
            {Key: model.SettingKeyCircuitBreakerCooldown, Value: "90"},
            {Key: model.SettingKeyCircuitBreakerMaxCooldown, Value: "900"},
        },
    }

    result, err := DBImportIncremental(ctx, dump)
    if err != nil {
        t.Fatalf("DBImportIncremental failed: %v", err)
    }
    if got := result.RowsAffected["settings"]; got != 0 {
        t.Fatalf("retired settings import rows=%d, want 0", got)
    }

    var count int64
    keys := []model.SettingKey{
        model.SettingKeyCircuitBreakerThreshold,
        model.SettingKeyCircuitBreakerCooldown,
        model.SettingKeyCircuitBreakerMaxCooldown,
    }
    if err := dbpkg.GetDB().Model(&model.Setting{}).Where("key IN ?", keys).Count(&count).Error; err != nil {
        t.Fatalf("count retired settings: %v", err)
    }
    if count != 0 {
        t.Fatalf("retired settings were reintroduced by import: %d rows", count)
    }
}

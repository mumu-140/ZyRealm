package model

import "testing"

func TestRelayAttemptBudgetSettingsValidation(t *testing.T) {
	keys := []SettingKey{
		SettingKeyRelayMaxProviderAttempts,
		SettingKeyRelayMaxWireAttempts,
	}
	for _, key := range keys {
		for _, value := range []string{"1", "20", "50", "1000"} {
			setting := Setting{Key: key, Value: value}
			if err := setting.Validate(); err != nil {
				t.Fatalf("key %s value %s should be valid: %v", key, value, err)
			}
		}
		for _, value := range []string{"0", "-1", "not-an-int"} {
			setting := Setting{Key: key, Value: value}
			if err := setting.Validate(); err == nil {
				t.Fatalf("key %s value %s should be rejected", key, value)
			}
		}
	}
}

func TestRelayAttemptBudgetDefaultsAreTwenty(t *testing.T) {
	defaults := map[SettingKey]string{}
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}
	for _, key := range []SettingKey{SettingKeyRelayMaxProviderAttempts, SettingKeyRelayMaxWireAttempts} {
		value, ok := defaults[key]
		if !ok {
			t.Fatalf("%s missing from default settings", key)
		}
		if value != "20" {
			t.Fatalf("default %s = %s, want 20", key, value)
		}
	}
}

func TestCircuitBreakerSettingsAreRetired(t *testing.T) {
	retired := []SettingKey{
		SettingKeyCircuitBreakerThreshold,
		SettingKeyCircuitBreakerCooldown,
		SettingKeyCircuitBreakerMaxCooldown,
	}

	defaults := map[SettingKey]string{}
	for _, setting := range DefaultSettings() {
		defaults[setting.Key] = setting.Value
	}

	for _, key := range retired {
		if _, ok := defaults[key]; ok {
			t.Fatalf("retired setting %s must not be seeded as an active default", key)
		}
		if err := (Setting{Key: key, Value: "1"}).Validate(); err == nil {
			t.Fatalf("retired setting %s must be rejected by validation", key)
		}
	}
}

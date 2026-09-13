package model

import "testing"

func TestRelayMaxWireAttemptsSettingValidation(t *testing.T) {
	for _, value := range []string{"1", "20"} {
		setting := Setting{Key: SettingKeyRelayMaxWireAttempts, Value: value}
		if err := setting.Validate(); err != nil {
			t.Fatalf("value %s should be valid: %v", value, err)
		}
	}
	for _, value := range []string{"0", "21", "not-an-int"} {
		setting := Setting{Key: SettingKeyRelayMaxWireAttempts, Value: value}
		if err := setting.Validate(); err == nil {
			t.Fatalf("value %s should be rejected", value)
		}
	}
}

func TestRelayMaxWireAttemptsDefaultIsTwenty(t *testing.T) {
	for _, setting := range DefaultSettings() {
		if setting.Key == SettingKeyRelayMaxWireAttempts {
			if setting.Value != "20" {
				t.Fatalf("default relay wire budget = %s, want 20", setting.Value)
			}
			return
		}
	}
	t.Fatal("relay_max_wire_attempts missing from default settings")
}

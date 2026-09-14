package model

import "testing"

func TestDefaultSettingsIncludeDisabledModelFilterRegex(t *testing.T) {
	for _, setting := range DefaultSettings() {
		if setting.Key == SettingKeyModelFilterRegex {
			if setting.Value != "" {
				t.Fatalf("default model filter = %q, want empty", setting.Value)
			}
			return
		}
	}
	t.Fatal("DefaultSettings() missing model_filter_regex")
}

func TestModelFilterRegexSettingValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "disabled", value: "", wantErr: false},
		{name: "valid", value: `^gpt-(4o|4\\.1)$`, wantErr: false},
		{name: "whitespace is a real regex", value: " ", wantErr: false},
		{name: "invalid", value: `(`, wantErr: true},
		{name: "preserve ECMAScript dialect", value: `a++`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&Setting{Key: SettingKeyModelFilterRegex, Value: tt.value}).Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUnrelatedSettingValidationRemainsUnchanged(t *testing.T) {
	if err := (&Setting{Key: SettingKeySyncLLMInterval, Value: "not-an-integer"}).Validate(); err == nil {
		t.Fatal("existing integer setting accepted invalid value")
	}
}

package model

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/headerutil"
	"gorm.io/gorm"
)

func (c *Channel) BeforeSave(_ *gorm.DB) error {
	if c == nil {
		return nil
	}
	return ValidateChannelCustomHeaders(c.CustomHeader)
}

func ValidateChannelCustomHeaders(headers []CustomHeader) error {
	for index, header := range headers {
		if !headerutil.HasClientHeaderTemplate(header.HeaderValue) {
			continue
		}
		key := strings.TrimSpace(header.HeaderKey)
		if !headerutil.IsAllowedClientHeaderTemplateTarget(key) {
			return fmt.Errorf("custom header %d has an invalid or protected template target", index+1)
		}
		if err := headerutil.ValidateClientHeaderTemplate(header.HeaderValue); err != nil {
			return fmt.Errorf("custom header %d (%s) has invalid client_header template: %w", index+1, key, err)
		}
	}
	return nil
}

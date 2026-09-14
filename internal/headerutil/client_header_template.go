package headerutil

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"
)

const clientHeaderTemplatePrefix = "{client_header:"

type ClientHeaderTemplateResult struct {
	Value  string
	Apply  bool
	Reason string
}

var blockedClientHeaderTemplateSources = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"api-key":             {},
	"anthropic-api-key":   {},
	"x-goog-api-key":      {},
	"cookie":              {},
	"set-cookie":          {},
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"content-length":      {},
	"host":                {},
	"accept-encoding":     {},
	"x-forwarded-for":     {},
	"x-forwarded-host":    {},
	"x-forwarded-proto":   {},
	"x-forwarded-port":    {},
	"x-real-ip":           {},
	"forwarded":           {},
	"cf-connecting-ip":    {},
	"true-client-ip":      {},
	"x-client-ip":         {},
	"x-cluster-client-ip": {},
	"origin":              {},
}

var blockedClientHeaderTemplateTargets = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"api-key":             {},
	"anthropic-api-key":   {},
	"x-goog-api-key":      {},
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"content-length":      {},
	"host":                {},
}

func HasClientHeaderTemplate(value string) bool {
	return strings.Contains(value, clientHeaderTemplatePrefix)
}

func IsAllowedClientHeaderTemplateSource(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !httpguts.ValidHeaderFieldName(name) {
		return false
	}
	lower := strings.ToLower(name)
	if _, blocked := blockedClientHeaderTemplateSources[lower]; blocked {
		return false
	}
	if strings.HasPrefix(lower, "sec-websocket-") || strings.HasPrefix(lower, "proxy-") {
		return false
	}
	return true
}

func IsAllowedClientHeaderTemplateTarget(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !httpguts.ValidHeaderFieldName(name) {
		return false
	}
	lower := strings.ToLower(name)
	if _, blocked := blockedClientHeaderTemplateTargets[lower]; blocked {
		return false
	}
	if strings.HasPrefix(lower, "sec-websocket-") || strings.HasPrefix(lower, "proxy-") {
		return false
	}
	return true
}

func ValidateClientHeaderTemplate(value string) error {
	_, err := walkClientHeaderTemplate(value, nil)
	return err
}

func RenderClientHeaderTemplate(value string, source http.Header) ClientHeaderTemplateResult {
	if !HasClientHeaderTemplate(value) {
		return ClientHeaderTemplateResult{Value: value, Apply: true}
	}

	rendered, err := walkClientHeaderTemplate(value, func(name string) (string, error) {
		if !IsAllowedClientHeaderTemplateSource(name) {
			return "", fmt.Errorf("blocked_source")
		}
		if source == nil {
			return "", fmt.Errorf("missing_source")
		}
		value, ok := headerValueEqualFold(source, name)
		if !ok {
			return "", fmt.Errorf("missing_source")
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return "", fmt.Errorf("invalid_source_value")
		}
		return value, nil
	})
	if err != nil {
		return ClientHeaderTemplateResult{Apply: false, Reason: templateReason(err)}
	}
	if !httpguts.ValidHeaderFieldValue(rendered) {
		return ClientHeaderTemplateResult{Apply: false, Reason: "invalid_rendered_value"}
	}
	return ClientHeaderTemplateResult{Value: rendered, Apply: true}
}

func SnapshotClientHeaderTemplateSource(src http.Header) http.Header {
	if len(src) == 0 {
		return nil
	}
	result := make(http.Header)
	for name, values := range src {
		if !IsAllowedClientHeaderTemplateSource(name) {
			continue
		}
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(name))
		for _, value := range values {
			if httpguts.ValidHeaderFieldValue(value) {
				result.Add(canonical, value)
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func headerValueEqualFold(source http.Header, name string) (string, bool) {
	canonical := http.CanonicalHeaderKey(name)
	if values, ok := source[canonical]; ok && len(values) > 0 {
		return values[0], true
	}
	for key, values := range source {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0], true
		}
	}
	return "", false
}

func walkClientHeaderTemplate(value string, resolve func(string) (string, error)) (string, error) {
	if !HasClientHeaderTemplate(value) {
		return value, nil
	}

	var out strings.Builder
	rest := value
	for {
		start := strings.Index(rest, clientHeaderTemplatePrefix)
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		placeholder := rest[start+len(clientHeaderTemplatePrefix):]
		end := strings.IndexByte(placeholder, '}')
		if end < 0 {
			return "", fmt.Errorf("malformed_template")
		}
		name := strings.TrimSpace(placeholder[:end])
		if name == "" || !httpguts.ValidHeaderFieldName(name) {
			return "", fmt.Errorf("invalid_source_name")
		}
		if !IsAllowedClientHeaderTemplateSource(name) {
			return "", fmt.Errorf("blocked_source")
		}
		if resolve != nil {
			replacement, err := resolve(name)
			if err != nil {
				return "", err
			}
			out.WriteString(replacement)
		} else {
			out.WriteString(rest[start : start+len(clientHeaderTemplatePrefix)+end+1])
		}
		rest = placeholder[end+1:]
	}
	return out.String(), nil
}

func templateReason(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case "malformed_template", "invalid_source_name", "blocked_source", "missing_source", "invalid_source_value":
		return err.Error()
	default:
		return "invalid_template"
	}
}

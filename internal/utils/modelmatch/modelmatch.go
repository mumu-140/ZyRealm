package modelmatch

import "github.com/dlclark/regexp2"

// Validate checks a model filter pattern using the same regexp dialect as
// Channel.MatchRegex. Only the exact empty string disables filtering.
func Validate(pattern string) error {
	if pattern == "" {
		return nil
	}
	_, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	return err
}

// Filter keeps models that match every non-empty pattern while preserving the
// input order and duplicates.
func Filter(models []string, patterns ...string) ([]string, error) {
	regexps := make([]*regexp2.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		regexps = append(regexps, re)
	}
	if len(regexps) == 0 {
		return models, nil
	}

	filtered := make([]string, 0, len(models))
	for _, name := range models {
		matchedAll := true
		for _, re := range regexps {
			matched, err := re.MatchString(name)
			if err != nil {
				return nil, err
			}
			if !matched {
				matchedAll = false
				break
			}
		}
		if matchedAll {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

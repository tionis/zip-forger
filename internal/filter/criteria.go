package filter

import (
	"path"
	"regexp"
	"strings"
)

type Criteria struct {
	IncludeGlobs []string `json:"includeGlobs" yaml:"includeGlobs"`
	ExcludeGlobs []string `json:"excludeGlobs" yaml:"excludeGlobs"`
	Extensions   []string `json:"extensions" yaml:"extensions"`
	PathPrefixes []string `json:"pathPrefixes" yaml:"pathPrefixes"`
}

func (c Criteria) IsZero() bool {
	return len(c.IncludeGlobs) == 0 &&
		len(c.ExcludeGlobs) == 0 &&
		len(c.Extensions) == 0 &&
		len(c.PathPrefixes) == 0
}

func Merge(base, adhoc Criteria) Criteria {
	return Criteria{
		IncludeGlobs: append(append([]string{}, base.IncludeGlobs...), adhoc.IncludeGlobs...),
		ExcludeGlobs: append(append([]string{}, base.ExcludeGlobs...), adhoc.ExcludeGlobs...),
		Extensions:   append(append([]string{}, base.Extensions...), adhoc.Extensions...),
		PathPrefixes: append(append([]string{}, base.PathPrefixes...), adhoc.PathPrefixes...),
	}
}

type Compiled struct {
	include    []*regexp.Regexp
	exclude    []*regexp.Regexp
	extensions map[string]struct{}
	prefixes   []string
}

func Compile(criteria Criteria) (Compiled, error) {
	criteria = normalize(criteria)
	out := Compiled{
		extensions: make(map[string]struct{}, len(criteria.Extensions)),
		prefixes:   make([]string, 0, len(criteria.PathPrefixes)),
	}

	for _, pattern := range criteria.IncludeGlobs {
		re, err := globToRegexp(pattern)
		if err != nil {
			return Compiled{}, err
		}
		out.include = append(out.include, re)
	}
	for _, pattern := range criteria.ExcludeGlobs {
		re, err := globToRegexp(pattern)
		if err != nil {
			return Compiled{}, err
		}
		out.exclude = append(out.exclude, re)
	}
	for _, ext := range criteria.Extensions {
		out.extensions[ext] = struct{}{}
	}
	for _, prefix := range criteria.PathPrefixes {
		out.prefixes = append(out.prefixes, strings.TrimSuffix(prefix, "/"))
	}

	return out, nil
}

func (c Compiled) Match(filePath string) bool {
	filePath = normalizePath(filePath)
	if filePath == "" {
		return false
	}

	if len(c.include) > 0 {
		matched := false
		for _, re := range c.include {
			if re.MatchString(filePath) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	for _, re := range c.exclude {
		if re.MatchString(filePath) {
			return false
		}
	}

	if len(c.extensions) > 0 {
		ext := strings.ToLower(path.Ext(filePath))
		if _, ok := c.extensions[ext]; !ok {
			return false
		}
	}

	if len(c.prefixes) > 0 {
		matched := false
		for _, prefix := range c.prefixes {
			if filePath == prefix || strings.HasPrefix(filePath, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func normalize(criteria Criteria) Criteria {
	criteria.IncludeGlobs = normalizeList(criteria.IncludeGlobs)
	criteria.ExcludeGlobs = normalizeList(criteria.ExcludeGlobs)
	criteria.PathPrefixes = normalizeList(criteria.PathPrefixes)
	criteria.Extensions = normalizeExtensions(criteria.Extensions)
	return criteria
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizePath(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeExtensions(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		out = append(out, value)
	}
	return out
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	value = path.Clean("/" + value)
	value = strings.TrimPrefix(value, "/")
	if value == "." {
		return ""
	}
	return value
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return regexp.Compile("^$")
	}

	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(pattern); {
		ch := pattern[i]

		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					// `**/` matches zero or more directory segments.
					b.WriteString("(?:.*/)?")
					i += 3
				} else {
					b.WriteString(".*")
					i += 2
				}
			} else {
				b.WriteString("[^/]*")
				i++
			}
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			if strings.ContainsRune(`.+()|[]{}^$\\`, rune(ch)) {
				b.WriteByte('\\')
			}
			b.WriteByte(ch)
			i++
		}
	}

	b.WriteString("$")
	return regexp.Compile(b.String())
}

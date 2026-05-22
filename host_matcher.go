package arc

import (
	"errors"
	"net"
	"net/netip"
	"strings"

	"github.com/ryanfowler/match"
	"github.com/ryanfowler/match/dns"
	"golang.org/x/net/idna"
)

const (
	maxDNSHostLength  = 253
	maxDNSLabelLength = 63
)

type hostMatcher[T any] struct {
	dnsRoutes dns.Router[T]
	ipRoutes  map[string]T
}

type hostLabelPattern struct {
	text      string
	paramName string
}

func (m *hostMatcher[T]) TryInsert(pattern string, value T) error {
	host, ipLiteral, err := normalizeHostPattern(pattern)
	if err != nil {
		return err
	}

	if ipLiteral {
		if m.ipRoutes == nil {
			m.ipRoutes = make(map[string]T)
		}
		if _, ok := m.ipRoutes[host]; ok {
			return &match.ConflictError{
				Route: host,
				With:  host,
			}
		}
		m.ipRoutes[host] = value
		return nil
	}

	if err := m.dnsRoutes.TryInsert(host, value); err != nil {
		return hostInsertError(err)
	}
	return nil
}

func hostInsertError(err error) error {
	var conflict *dns.ConflictError
	if errors.As(err, &conflict) {
		return &match.ConflictError{
			Route: conflict.Pattern,
			With:  conflict.With,
		}
	}
	return ErrInvalidHostPattern
}

func (m *hostMatcher[T]) Match(host string) (T, match.Params, bool) {
	if isIPv6Literal(host) {
		if m.ipRoutes != nil {
			if value, ok := m.ipRoutes[host]; ok {
				return value, match.Params{}, true
			}
		}
		var zero T
		return zero, match.Params{}, false
	}
	return m.dnsRoutes.Match(host)
}

func normalizeHostPattern(pattern string) (string, bool, error) {
	if pattern == "" {
		return "", false, ErrInvalidHostPattern
	}
	if h, port, err := net.SplitHostPort(pattern); err == nil && h != "" && port != "" {
		return "", false, ErrInvalidHostPattern
	}

	host := normalizeHostAddress(pattern)
	if isIPv6Literal(host) {
		return strings.ToLower(host), true, nil
	}
	if strings.ContainsAny(host, "[]:") {
		return "", false, ErrInvalidHostPattern
	}

	host = trimTrailingHostDot(host)
	if host == "" {
		return "", false, ErrInvalidHostPattern
	}

	var labels []hostLabelPattern
	var seenNames [8]string
	seenCount := 0
	var seenMap map[string]struct{}
	for labelIndex, start := 0, 0; start <= len(host); labelIndex++ {
		end := start
		for end < len(host) && host[end] != '.' {
			end++
		}
		if end == start {
			return "", false, ErrInvalidHostPattern
		}

		label, err := normalizeHostPatternLabel(host[start:end], labelIndex)
		if err != nil {
			return "", false, err
		}
		if label.paramName != "" {
			if err := checkHostParamName(label.paramName, &seenNames, &seenCount, &seenMap); err != nil {
				return "", false, err
			}
		}
		labels = append(labels, label)

		if end == len(host) {
			break
		}
		start = end + 1
	}

	if len(labels) == 0 {
		return "", false, ErrInvalidHostPattern
	}
	return joinHostPattern(labels, "."), false, nil
}

func normalizeHostPatternLabel(label string, labelIndex int) (hostLabelPattern, error) {
	if strings.ContainsAny(label, "{}") {
		return normalizeDynamicHostPatternLabel(label, labelIndex)
	}

	literal, ok := normalizeHostLiteralLabel(label)
	if !ok {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}
	return hostLabelPattern{text: literal}, nil
}

func normalizeDynamicHostPatternLabel(label string, labelIndex int) (hostLabelPattern, error) {
	var prefix strings.Builder
	var suffix strings.Builder
	paramSeen := false
	paramRaw := ""
	paramName := ""
	catchAll := false

	writeLiteral := func(c byte) {
		if paramSeen {
			suffix.WriteByte(c)
			return
		}
		prefix.WriteByte(c)
	}

	for i := 0; i < len(label); {
		switch label[i] {
		case '{':
			if i+1 < len(label) && label[i+1] == '{' {
				writeLiteral('{')
				i += 2
				continue
			}
			if paramSeen {
				return hostLabelPattern{}, ErrInvalidHostPattern
			}
			end, err := findPatternParamEnd(label, i+1)
			if err != nil {
				return hostLabelPattern{}, ErrInvalidHostPattern
			}

			name := unescapePatternParamName(label[i+1 : end])
			if name == "" {
				return hostLabelPattern{}, ErrInvalidHostPattern
			}
			if name[0] == '*' {
				catchAll = true
				name = name[1:]
				if name == "" || labelIndex != 0 {
					return hostLabelPattern{}, ErrInvalidHostPattern
				}
			}
			paramSeen = true
			paramRaw = label[i : end+1]
			paramName = name
			i = end + 1
		case '}':
			if i+1 < len(label) && label[i+1] == '}' {
				writeLiteral('}')
				i += 2
				continue
			}
			return hostLabelPattern{}, ErrInvalidHostPattern
		default:
			writeLiteral(label[i])
			i++
		}
	}

	if !paramSeen {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}
	if catchAll && suffix.Len() != 0 {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}

	prefixText, ok := normalizeDynamicHostPatternLiteral(prefix.String())
	if !ok || (prefixText != "" && prefixText[0] == '-') {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}
	suffixText, ok := normalizeDynamicHostPatternLiteral(suffix.String())
	if !ok || (suffixText != "" && suffixText[len(suffixText)-1] == '-') {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}

	literalLength := len(prefixText) + len(suffixText)
	if catchAll {
		if literalLength >= maxDNSLabelLength {
			return hostLabelPattern{}, ErrInvalidHostPattern
		}
	} else if literalLength >= maxDNSLabelLength {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}

	return hostLabelPattern{
		text:      prefixText + paramRaw + suffixText,
		paramName: paramName,
	}, nil
}

func normalizeDynamicHostPatternLiteral(s string) (string, bool) {
	if s == "" {
		return "", true
	}

	needsLower := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
			needsLower = true
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return "", false
		}
	}
	if needsLower {
		return strings.ToLower(s), true
	}
	return s, true
}

func checkHostParamName(name string, seenNames *[8]string, seenCount *int, seenMap *map[string]struct{}) error {
	if *seenMap != nil {
		if _, ok := (*seenMap)[name]; ok {
			return ErrDuplicateParamName
		}
		(*seenMap)[name] = struct{}{}
		return nil
	}

	for i := 0; i < *seenCount; i++ {
		if seenNames[i] == name {
			return ErrDuplicateParamName
		}
	}
	if *seenCount < len(seenNames) {
		seenNames[*seenCount] = name
		(*seenCount)++
		return nil
	}

	*seenMap = make(map[string]struct{}, *seenCount+1)
	for i := 0; i < *seenCount; i++ {
		(*seenMap)[seenNames[i]] = struct{}{}
	}
	(*seenMap)[name] = struct{}{}
	return nil
}

func joinHostPattern(labels []hostLabelPattern, sep string) string {
	if len(labels) == 1 {
		return labels[0].text
	}

	var n int
	for i := range labels {
		n += len(labels[i].text)
	}
	n += len(sep) * (len(labels) - 1)

	var b strings.Builder
	b.Grow(n)
	for i := range labels {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(labels[i].text)
	}
	return b.String()
}

func normalizeRequestHost(host string) string {
	host = requestHostWithoutPort(host)
	host = normalizeHostAddress(host)
	if isIPv6Literal(host) {
		return strings.ToLower(host)
	}

	normalized, ok := normalizeDNSHost(host)
	if !ok {
		return ""
	}
	return normalized
}

func requestHostWithoutPort(host string) string {
	if host == "" {
		return ""
	}
	if strings.IndexByte(host, ':') == -1 {
		return host
	}
	if h, port, err := net.SplitHostPort(host); err == nil {
		if h == "" || port == "" || !isASCIIDigits(port) {
			return host
		}
		return h
	}

	i := strings.LastIndexByte(host, ':')
	if i <= 0 || strings.IndexByte(host[:i], ':') != -1 {
		return host
	}
	if i+1 == len(host) || !isASCIIDigits(host[i+1:]) {
		return host
	}
	return host[:i]
}

func normalizeHostAddress(host string) string {
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' && strings.IndexByte(host, ':') != -1 {
		return host[1 : len(host)-1]
	}
	return host
}

func normalizeHostLiteralLabel(label string) (string, bool) {
	normalized, ok := normalizeDNSHost(label)
	if !ok || strings.IndexByte(normalized, '.') != -1 {
		return "", false
	}
	return normalized, true
}

func normalizeDNSHost(host string) (string, bool) {
	host = trimTrailingHostDot(host)
	if host == "" {
		return "", false
	}
	if normalized, ok := normalizeASCIIHost(host, true); ok {
		return normalized, true
	}

	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", false
	}
	ascii = strings.ToLower(trimTrailingHostDot(ascii))
	if normalized, ok := normalizeASCIIHost(ascii, false); ok {
		return normalized, true
	}
	return "", false
}

func normalizeASCIIHost(host string, checkACE bool) (string, bool) {
	if len(host) > maxDNSHostLength {
		return "", false
	}

	needsLower := false
	labelStart := 0
	for i := 0; i <= len(host); i++ {
		if i == len(host) || host[i] == '.' {
			if !validASCIIHostLabel(host[labelStart:i], checkACE) {
				return "", false
			}
			labelStart = i + 1
			continue
		}

		c := host[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
			needsLower = true
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return "", false
		}
	}

	if needsLower {
		return strings.ToLower(host), true
	}
	return host, true
}

func validASCIIHostLabel(label string, checkACE bool) bool {
	if label == "" || len(label) > maxDNSLabelLength {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	if checkACE && hasACEPrefix(label) {
		return false
	}
	return true
}

func hasACEPrefix(label string) bool {
	return len(label) >= 4 &&
		(label[0] == 'x' || label[0] == 'X') &&
		(label[1] == 'n' || label[1] == 'N') &&
		label[2] == '-' &&
		label[3] == '-'
}

func trimTrailingHostDot(host string) string {
	if host != "" && host[len(host)-1] == '.' {
		return host[:len(host)-1]
	}
	return host
}

func isIPv6Literal(host string) bool {
	if strings.IndexByte(host, ':') == -1 {
		return false
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Is6() && addr.Zone() == ""
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

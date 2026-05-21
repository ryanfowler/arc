package arc

import (
	"errors"
	"net"
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
	dnsRoutes          dns.Router[T]
	ipRoutes           map[string]T
	routesByLabelCount map[int][]hostRoute
}

type hostRoute struct {
	pattern string
	labels  []hostLabelPattern
}

type hostLabelPattern struct {
	literal   string
	paramName string
	rawParam  string
}

type hostLabelMatch uint8

const (
	hostLabelSame hostLabelMatch = iota
	hostLabelAMoreSpecific
	hostLabelBMoreSpecific
	hostLabelDisjoint
)

func (m *hostMatcher[T]) TryInsert(pattern string, value T) error {
	labels, err := parseHostPattern(pattern)
	if err != nil {
		return err
	}

	route := hostRoute{
		pattern: joinHostPattern(labels, "."),
		labels:  labels,
	}
	if existing := m.findConflict(route); existing != nil {
		return &match.ConflictError{
			Route: route.pattern,
			With:  existing.pattern,
		}
	}

	if isIPv6Literal(route.pattern) {
		if m.ipRoutes == nil {
			m.ipRoutes = make(map[string]T)
		}
		m.ipRoutes[route.pattern] = value
		m.index(route)
		return nil
	}

	if err := m.dnsRoutes.TryInsert(route.pattern, value); err != nil {
		return hostInsertError(err)
	}
	m.index(route)
	return nil
}

func (m *hostMatcher[T]) findConflict(route hostRoute) *hostRoute {
	for i := range m.routesByLabelCount[len(route.labels)] {
		existing := &m.routesByLabelCount[len(route.labels)][i]
		if hostRoutesConflict(existing, &route) {
			return existing
		}
	}
	return nil
}

func (m *hostMatcher[T]) index(route hostRoute) {
	labelCount := len(route.labels)
	if m.routesByLabelCount == nil {
		m.routesByLabelCount = make(map[int][]hostRoute)
	}
	m.routesByLabelCount[labelCount] = append(m.routesByLabelCount[labelCount], route)
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

func hostRoutesConflict(a, b *hostRoute) bool {
	if len(a.labels) != len(b.labels) {
		return false
	}

	aMoreSpecific := false
	bMoreSpecific := false
	for i := range a.labels {
		switch hostLabelSpecificity(a.labels[i], b.labels[i]) {
		case hostLabelDisjoint:
			return false
		case hostLabelAMoreSpecific:
			aMoreSpecific = true
		case hostLabelBMoreSpecific:
			bMoreSpecific = true
		}
	}

	return aMoreSpecific == bMoreSpecific
}

func hostLabelSpecificity(a, b hostLabelPattern) hostLabelMatch {
	aParam := a.paramName != ""
	bParam := b.paramName != ""
	switch {
	case !aParam && !bParam:
		if a.literal != b.literal {
			return hostLabelDisjoint
		}
	case !aParam && bParam:
		return hostLabelAMoreSpecific
	case aParam && !bParam:
		return hostLabelBMoreSpecific
	}
	return hostLabelSame
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

func parseHostPattern(pattern string) ([]hostLabelPattern, error) {
	if pattern == "" {
		return nil, ErrInvalidHostPattern
	}
	if h, port, err := net.SplitHostPort(pattern); err == nil && h != "" && port != "" {
		return nil, ErrInvalidHostPattern
	}

	host := normalizeHostAddress(pattern)
	if isIPv6Literal(host) {
		return []hostLabelPattern{{literal: strings.ToLower(host)}}, nil
	}
	if strings.ContainsAny(host, "[]:") {
		return nil, ErrInvalidHostPattern
	}

	host = trimTrailingHostDot(host)
	if host == "" {
		return nil, ErrInvalidHostPattern
	}

	var labels []hostLabelPattern
	var seenNames [8]string
	seenCount := 0
	var seenMap map[string]struct{}
	for start := 0; start <= len(host); {
		end := start
		for end < len(host) && host[end] != '.' {
			end++
		}
		if end == start {
			return nil, ErrInvalidHostPattern
		}

		label, err := normalizeHostPatternLabel(host[start:end])
		if err != nil {
			return nil, err
		}
		if label.paramName != "" {
			if err := checkHostParamName(label.paramName, &seenNames, &seenCount, &seenMap); err != nil {
				return nil, err
			}
		}
		labels = append(labels, label)

		if end == len(host) {
			break
		}
		start = end + 1
	}

	if len(labels) == 0 {
		return nil, ErrInvalidHostPattern
	}
	if minHostPatternLength(labels) > maxDNSHostLength {
		return nil, ErrInvalidHostPattern
	}
	return labels, nil
}

func normalizeHostPatternLabel(label string) (hostLabelPattern, error) {
	if strings.ContainsAny(label, "{}") {
		name, ok := parseHostParamLabel(label)
		if !ok {
			return hostLabelPattern{}, ErrInvalidHostPattern
		}
		return hostLabelPattern{
			paramName: name,
			rawParam:  label,
		}, nil
	}

	literal, ok := normalizeHostLiteralLabel(label)
	if !ok {
		return hostLabelPattern{}, ErrInvalidHostPattern
	}
	return hostLabelPattern{literal: literal}, nil
}

func parseHostParamLabel(label string) (string, bool) {
	if len(label) < 3 || label[0] != '{' {
		return "", false
	}
	end, err := findPatternParamEnd(label, 1)
	if err != nil || end != len(label)-1 {
		return "", false
	}
	name := unescapePatternParamName(label[1:end])
	if name == "" || name[0] == '*' {
		return "", false
	}
	return name, true
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
		return labels[0].patternText()
	}

	var n int
	for i := range labels {
		n += len(labels[i].patternText())
	}
	n += len(sep) * (len(labels) - 1)

	var b strings.Builder
	b.Grow(n)
	for i := range labels {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(labels[i].patternText())
	}
	return b.String()
}

func (p hostLabelPattern) patternText() string {
	if p.paramName != "" {
		return p.rawParam
	}
	return p.literal
}

func minHostPatternLength(labels []hostLabelPattern) int {
	length := len(labels) - 1
	for i := range labels {
		if labels[i].paramName != "" {
			length++
			continue
		}
		length += len(labels[i].literal)
	}
	return length
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
	return strings.IndexByte(host, ':') != -1 && net.ParseIP(host) != nil
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

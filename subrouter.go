package arc

import (
	"net/http"
	"strings"

	"github.com/ryanfowler/match"
)

// SubRouter registers and returns a child router mounted at pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Parameters
// captured by the mount pattern are available to child handlers.
//
// The child matches against the remaining path after the mount point. For
// example, a child mounted at /api matches /users for a request to /api/users,
// while both /api and /api/ are dispatched to the child's / route. The request
// URL is not rewritten; middleware and handlers still see the original
// req.URL.Path.
//
// Middleware already registered on the parent wraps the child router.
// Middleware added to the child applies only inside the child router.
//
// Invalid, duplicate, or ambiguous mount patterns panic with the error returned
// by match. Use SubRouterErr to receive the registration error instead.
func (r *Router) SubRouter(pattern string) *Router {
	child, err := r.SubRouterErr(pattern)
	if err != nil {
		panic(err)
	}
	return child
}

// SubRouterErr registers and returns a child router mounted at pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Registration
// errors include invalid parameter syntax and mount conflicts reported by
// match.
func (r *Router) SubRouterErr(pattern string) (*Router, error) {
	child := newChildRouter(r)
	pattern = cleanMountPattern(pattern)

	if err := r.subMounts.TryInsert(pattern, child); err != nil {
		return nil, err
	}

	child.router.patternPrefix = joinPatterns(r.patternPrefix, pattern)
	r.hasSubRouters = true
	return child.router, nil
}

// Mount registers h below pattern.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Parameters
// captured by the mount pattern are available to middleware and the mounted
// handler.
//
// The mounted handler receives the remaining path after the mount point as
// req.URL.Path. For example, a handler mounted at /assets receives /app.css for
// a request to /assets/app.css, while both /assets and /assets/ are dispatched
// as /. Middleware already registered on the parent sees the original request
// path and wraps the mounted handler.
//
// Invalid, duplicate, or ambiguous mount patterns panic with the error returned
// by match. Use MountErr to receive the registration error instead.
func (r *Router) Mount(pattern string, h http.Handler) {
	if err := r.MountErr(pattern, h); err != nil {
		panic(err)
	}
}

// MountErr registers h below pattern and returns registration errors.
//
// The pattern uses the github.com/ryanfowler/match route grammar. Registration
// errors include invalid parameter syntax and mount conflicts reported by
// match. A nil handler is treated as http.NotFoundHandler.
func (r *Router) MountErr(pattern string, h http.Handler) error {
	if h == nil {
		h = http.NotFoundHandler()
	}

	pattern = cleanMountPattern(pattern)
	child := &childRouter{
		router:  r,
		handler: compose(mountedHandler{handler: h}, r.middleware),
		mounted: true,
		pattern: joinPatterns(r.patternPrefix, pattern),
	}
	if err := r.subMounts.TryInsert(pattern, child); err != nil {
		return err
	}

	r.hasSubRouters = true
	return nil
}

type mountedHandler struct {
	handler http.Handler
}

func (h mountedHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path, _ := dispatchState(req)
	h.handler.ServeHTTP(w, requestWithURLPath(req, path))
}

type mountMatcher struct {
	validate match.Router[*childRouter]
	root     mountNode
}

func (m *mountMatcher) TryInsert(pattern string, child *childRouter) error {
	pattern = cleanMountPattern(pattern)
	segments, err := parseMountPattern(pattern)
	if err != nil {
		return err
	}
	if err := m.validate.TryInsert(pattern, child); err != nil {
		return err
	}

	m.root.insert(segments, child)
	return nil
}

func (m *mountMatcher) Match(path string) (*childRouter, string, match.Params, bool) {
	return m.root.match(path)
}

func cleanMountPattern(pattern string) string {
	if pattern == "" {
		return "/"
	}
	if pattern != "/" {
		pattern = strings.TrimRight(pattern, "/")
		if pattern == "" {
			return "/"
		}
	}
	return pattern
}

type mountNode struct {
	child                 *childRouter
	static                []mountStaticEdge
	staticIndex           map[string]*mountNode
	params                []mountParamEdge
	catchAll              []mountCatchAllEdge
	maxDescendantSegments int
	hasCatchAllDescendant bool
}

type mountStaticEdge struct {
	segment string
	child   *mountNode
}

type mountParamEdge struct {
	pattern mountSegment
	child   *mountNode
}

type mountCatchAllEdge struct {
	pattern mountSegment
	child   *childRouter
}

type mountSegment struct {
	literal  bool
	catchAll bool
	raw      string
	name     string
	prefix   string
	suffix   string
}

func (n *mountNode) insert(segments []mountSegment, router *childRouter) {
	current := n
	hasCatchAll := len(segments) > 0 && segments[len(segments)-1].catchAll
	if hasCatchAll {
		current.hasCatchAllDescendant = true
	}
	for i := range segments {
		segment := segments[i]
		if segment.catchAll {
			current.catchAll = append(current.catchAll, mountCatchAllEdge{
				pattern: segment,
				child:   router,
			})
			return
		}

		remaining := len(segments) - i
		if remaining > current.maxDescendantSegments {
			current.maxDescendantSegments = remaining
		}

		if segment.literal {
			child := current.staticChild(segment.raw)
			if child == nil {
				child = &mountNode{}
				current.addStaticChild(segment.raw, child)
			}
			current = child
		} else {
			var child *mountNode
			for j := range current.params {
				if sameMountSegmentPattern(current.params[j].pattern, segment) {
					child = current.params[j].child
					break
				}
			}
			if child == nil {
				child = &mountNode{}
				current.params = append(current.params, mountParamEdge{
					pattern: segment,
					child:   child,
				})
				sortMountParamEdges(current.params)
			}
			current = child
		}
		if hasCatchAll {
			current.hasCatchAllDescendant = true
		}
	}
	current.child = router
}

type mountMatch struct {
	child     *childRouter
	nextIndex int
	consumed  int
	params    match.Params
}

func (n *mountNode) match(path string) (*childRouter, string, match.Params, bool) {
	var inline [4]match.Param
	captures := inline[:0]
	best, ok := n.matchPath(path, mountMatchStart(path), captures)
	if !ok {
		return nil, "", match.Params{}, false
	}
	return best.child, remainingMountPath(path, best.nextIndex), best.params, true
}

func (n *mountNode) matchPath(path string, index int, captures []match.Param) (mountMatch, bool) {
	var best mountMatch
	if n.child != nil {
		best = mountMatch{
			child:     n.child,
			nextIndex: index,
			consumed:  consumedMountPath(path, index),
			params:    match.ParamsOf(captures...),
		}
	}

	if index >= 0 {
		segment, next := nextMountPathSegment(path, index)
		if child := n.staticChild(segment); child != nil {
			if child.canImproveMountMatch(path, next, best) {
				if candidate, ok := child.matchPath(path, next, captures); ok {
					best = betterMountMatch(best, candidate)
				}
			}
		}

		for i := range n.params {
			edge := n.params[i]
			value, ok := matchMountParam(edge.pattern, segment)
			if !ok {
				continue
			}
			if !edge.child.canImproveMountMatch(path, next, best) {
				continue
			}
			if !edge.child.canStartMountMatch(path, next) {
				continue
			}
			nextCaptures := appendMountParam(captures, edge.pattern.name, value)
			if candidate, ok := edge.child.matchPath(path, next, nextCaptures); ok {
				best = betterMountMatch(best, candidate)
			}
		}

		for i := range n.catchAll {
			edge := n.catchAll[i]
			if best.child != nil && best.consumed >= len(path)+1 {
				continue
			}
			if !matchMountCatchAll(edge.pattern, path[index:]) {
				continue
			}
			nextCaptures := appendMountParam(captures, edge.pattern.name, path[index+len(edge.pattern.prefix):])
			candidate := mountMatch{
				child:     edge.child,
				nextIndex: -1,
				consumed:  len(path) + 1,
				params:    match.ParamsOf(nextCaptures...),
			}
			best = betterMountMatch(best, candidate)
		}
	}

	return best, best.child != nil
}

func (n *mountNode) canImproveMountMatch(path string, index int, best mountMatch) bool {
	if best.child == nil {
		return true
	}
	if n.hasCatchAllDescendant {
		return best.consumed < len(path)+1
	}
	return maxConsumedMountPath(path, index, n.maxDescendantSegments) > best.consumed
}

func (n *mountNode) canStartMountMatch(path string, index int) bool {
	if n.child != nil {
		return true
	}
	if index < 0 {
		return false
	}

	segment, _ := nextMountPathSegment(path, index)
	if n.staticChild(segment) != nil {
		return true
	}
	for i := range n.params {
		if _, ok := matchMountParam(n.params[i].pattern, segment); ok {
			return true
		}
	}
	for i := range n.catchAll {
		if matchMountCatchAll(n.catchAll[i].pattern, path[index:]) {
			return true
		}
	}
	return false
}

func maxConsumedMountPath(path string, index int, segments int) int {
	consumed := consumedMountPath(path, index)
	for range segments {
		if index < 0 {
			return consumed
		}
		_, index = nextMountPathSegment(path, index)
		consumed = consumedMountPath(path, index)
	}
	return consumed
}

func betterMountMatch(best, candidate mountMatch) mountMatch {
	if best.child == nil || candidate.consumed > best.consumed {
		return candidate
	}
	return best
}

func (n *mountNode) staticChild(segment string) *mountNode {
	if n.staticIndex != nil {
		return n.staticIndex[segment]
	}
	for i := range n.static {
		if n.static[i].segment == segment {
			return n.static[i].child
		}
	}
	return nil
}

func (n *mountNode) addStaticChild(segment string, child *mountNode) {
	n.static = append(n.static, mountStaticEdge{segment: segment, child: child})
	if len(n.static) == 5 {
		n.staticIndex = make(map[string]*mountNode, len(n.static))
		for i := range n.static {
			n.staticIndex[n.static[i].segment] = n.static[i].child
		}
		return
	}
	if n.staticIndex != nil {
		n.staticIndex[segment] = child
	}
}

func sortMountParamEdges(edges []mountParamEdge) {
	for i := 1; i < len(edges); i++ {
		edge := edges[i]
		j := i - 1
		for j >= 0 && mountParamEdgeLess(edge, edges[j]) {
			edges[j+1] = edges[j]
			j--
		}
		edges[j+1] = edge
	}
}

func mountParamEdgeLess(a, b mountParamEdge) bool {
	aStatic := len(a.pattern.prefix) + len(a.pattern.suffix)
	bStatic := len(b.pattern.prefix) + len(b.pattern.suffix)
	if aStatic != bStatic {
		return aStatic > bStatic
	}
	return len(a.pattern.prefix) > len(b.pattern.prefix)
}

func sameMountSegmentPattern(a, b mountSegment) bool {
	return a.raw == b.raw &&
		a.literal == b.literal &&
		a.catchAll == b.catchAll &&
		a.name == b.name &&
		a.prefix == b.prefix &&
		a.suffix == b.suffix
}

func appendMountParam(params []match.Param, key, val string) []match.Param {
	return append(params, match.Param{Key: key, Val: val})
}

func matchMountParam(pattern mountSegment, segment string) (string, bool) {
	if !strings.HasPrefix(segment, pattern.prefix) || !strings.HasSuffix(segment, pattern.suffix) {
		return "", false
	}
	start := len(pattern.prefix)
	end := len(segment) - len(pattern.suffix)
	if end <= start {
		return "", false
	}
	return segment[start:end], true
}

func matchMountCatchAll(pattern mountSegment, rest string) bool {
	return strings.HasPrefix(rest, pattern.prefix) && len(rest) > len(pattern.prefix)
}

func mountMatchStart(path string) int {
	if path == "" || path[0] != '/' {
		return 0
	}
	return 1
}

func consumedMountPath(path string, index int) int {
	if index < 0 {
		return len(path) + 1
	}
	return index
}

func nextMountPathSegment(path string, index int) (string, int) {
	if index == len(path) {
		return "", -1
	}
	end := min(index+16, len(path))
	for i := index; i < end; i++ {
		if path[i] == '/' {
			return path[index:i], i + 1
		}
	}
	if end < len(path) {
		if i := strings.IndexByte(path[end:], '/'); i >= 0 {
			return path[index : end+i], end + i + 1
		}
	}
	return path[index:], -1
}

func remainingMountPath(path string, index int) string {
	if index < 0 {
		return "/"
	}
	if index > len(path) {
		return "/"
	}
	if index == len(path) {
		return "/"
	}
	if path[index] == '/' {
		if index == 1 && len(path) > 1 && path[0] == '/' {
			return "/" + path[index+1:]
		}
		return path[index:]
	}
	if index == 0 {
		return path
	}
	return path[index-1:]
}

func parseMountPattern(pattern string) ([]mountSegment, error) {
	if pattern == "/" {
		return nil, nil
	}

	parts := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	segments := make([]mountSegment, 0, len(parts))
	for i, part := range parts {
		segment, err := parseMountSegment(part)
		if err != nil {
			return nil, err
		}
		if segment.catchAll && i != len(parts)-1 {
			return nil, match.ErrInvalidCatchAll
		}
		segments = append(segments, segment)
	}
	return segments, nil
}

func parseMountSegment(segment string) (mountSegment, error) {
	var parsed mountSegment
	var literal strings.Builder
	paramSeen := false

	for i := 0; i < len(segment); {
		switch segment[i] {
		case '{':
			if i+1 < len(segment) && segment[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			if paramSeen {
				return mountSegment{}, match.ErrInvalidParamSegment
			}
			paramSeen = true
			parsed.prefix = literal.String()
			literal.Reset()

			name, catchAll, next, err := parseMountParamName(segment, i+1)
			if err != nil {
				return mountSegment{}, err
			}
			parsed.name = name
			parsed.catchAll = catchAll
			i = next
		case '}':
			if i+1 < len(segment) && segment[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return mountSegment{}, match.ErrInvalidParam
		default:
			literal.WriteByte(segment[i])
			i++
		}
	}

	if !paramSeen {
		parsed.literal = true
		parsed.raw = literal.String()
		return parsed, nil
	}
	parsed.suffix = literal.String()
	return parsed, nil
}

func parseMountParamName(segment string, index int) (string, bool, int, error) {
	var name strings.Builder
	for i := index; i < len(segment); {
		switch segment[i] {
		case '{':
			if i+1 < len(segment) && segment[i+1] == '{' {
				name.WriteByte('{')
				i += 2
				continue
			}
			return "", false, 0, match.ErrInvalidParam
		case '}':
			if i+1 < len(segment) && segment[i+1] == '}' {
				name.WriteByte('}')
				i += 2
				continue
			}
			raw := name.String()
			if after, ok := strings.CutPrefix(raw, "*"); ok {
				return after, true, i + 1, nil
			}
			return raw, false, i + 1, nil
		default:
			name.WriteByte(segment[i])
			i++
		}
	}
	return "", false, 0, match.ErrInvalidParam
}

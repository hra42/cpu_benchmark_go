package benchmarks

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"cpu_bench_go/runner"
)

const (
	ApacheSmall  = 100
	ApacheMedium = 1_000
	ApacheLarge  = 10_000
)

type apacheRoute struct {
	prefix      string
	exact       bool
	status      int
	contentType string
	body        string
}

type ApacheLike struct {
	n        int
	requests [][]byte
	routes   []apacheRoute
	respBuf  bytes.Buffer
	logBuf   bytes.Buffer
	hdrName  []byte
	hdrVal   []byte
	expected uint64
	// Per-request response slot, reused across iterations.
	headerNames  []string
	headerValues []string
}

func (a *ApacheLike) Name() string { return "apache_like" }

func (a *ApacheLike) Tags() []string { return []string{"string", "memory", "branch", "allocation"} }

func (a *ApacheLike) Setup(n int) {
	if n <= 0 {
		n = ApacheMedium
	}
	a.n = n
	a.requests = buildApacheRequests(n)
	a.routes = defaultApacheRoutes()
	a.respBuf.Grow(1024)
	a.logBuf.Grow(512)
	a.hdrName = make([]byte, 0, 64)
	a.hdrVal = make([]byte, 0, 256)
	a.headerNames = make([]string, 0, 16)
	a.headerValues = make([]string, 0, 16)
	a.expected = a.computeChecksum()
}

func (a *ApacheLike) Run() any {
	h := fnv.New64a()
	for i, raw := range a.requests {
		method, path, proto, ok := parseRequestLine(raw)
		if !ok {
			return uint64(0)
		}
		headerEnd, hOk := parseHeaders(raw, &a.headerNames, &a.headerValues, &a.hdrName, &a.hdrVal)
		if !hOk {
			return uint64(0)
		}
		_ = headerEnd

		// Find a couple of canonical headers via the parsed slices.
		host := lookupHeader(a.headerNames, a.headerValues, "Host")
		ua := lookupHeader(a.headerNames, a.headerValues, "User-Agent")
		_ = host
		_ = ua

		// Route match.
		route := matchRoute(a.routes, path)

		// Build response header block.
		a.respBuf.Reset()
		a.respBuf.WriteString(proto)
		a.respBuf.WriteByte(' ')
		a.respBuf.WriteString(strconv.Itoa(route.status))
		a.respBuf.WriteByte(' ')
		a.respBuf.WriteString(statusText(route.status))
		a.respBuf.WriteString("\r\n")
		a.respBuf.WriteString("Server: cpu-bench-apache/0.1\r\n")
		a.respBuf.WriteString("Date: Tue, 05 May 2026 12:00:00 GMT\r\n")
		a.respBuf.WriteString("Content-Type: ")
		a.respBuf.WriteString(route.contentType)
		a.respBuf.WriteString("\r\n")
		a.respBuf.WriteString("Content-Length: ")
		a.respBuf.WriteString(strconv.Itoa(len(route.body)))
		a.respBuf.WriteString("\r\n")
		a.respBuf.WriteString("Connection: keep-alive\r\n\r\n")
		a.respBuf.WriteString(route.body)

		// CLF access log: %h %l %u %t "%r" %>s %b "%{Referer}i" "%{User-Agent}i"
		a.logBuf.Reset()
		a.logBuf.WriteString("10.0.0.")
		a.logBuf.WriteString(strconv.Itoa(i & 0xff))
		a.logBuf.WriteString(" - - [05/May/2026:12:00:")
		writeTwoDigit(&a.logBuf, i%60)
		a.logBuf.WriteString(" +0000] \"")
		a.logBuf.WriteString(method)
		a.logBuf.WriteByte(' ')
		a.logBuf.WriteString(path)
		a.logBuf.WriteByte(' ')
		a.logBuf.WriteString(proto)
		a.logBuf.WriteString("\" ")
		a.logBuf.WriteString(strconv.Itoa(route.status))
		a.logBuf.WriteByte(' ')
		a.logBuf.WriteString(strconv.Itoa(len(route.body)))
		a.logBuf.WriteString(" \"-\" \"")
		a.logBuf.WriteString(ua)
		a.logBuf.WriteString("\"\n")

		h.Write(a.respBuf.Bytes())
		h.Write(a.logBuf.Bytes())
	}
	return h.Sum64()
}

func (a *ApacheLike) Verify(result any) bool {
	got, ok := result.(uint64)
	if !ok {
		return false
	}
	return got == a.expected
}

func (a *ApacheLike) computeChecksum() uint64 {
	v, _ := a.Run().(uint64)
	return v
}

// parseRequestLine: "METHOD SP PATH SP HTTP/1.1 CRLF"
func parseRequestLine(buf []byte) (method, path, proto string, ok bool) {
	end := bytes.Index(buf, []byte("\r\n"))
	if end < 0 {
		return "", "", "", false
	}
	line := buf[:end]
	sp1 := bytes.IndexByte(line, ' ')
	if sp1 < 0 {
		return "", "", "", false
	}
	sp2 := bytes.IndexByte(line[sp1+1:], ' ')
	if sp2 < 0 {
		return "", "", "", false
	}
	sp2 += sp1 + 1
	return string(line[:sp1]), string(line[sp1+1 : sp2]), string(line[sp2+1:]), true
}

// parseHeaders walks header lines after the request line, normalizing names to canonical form.
// Returns the offset just past the header terminator (the second CRLF).
func parseHeaders(buf []byte, names, values *[]string, nameBuf, valBuf *[]byte) (int, bool) {
	*names = (*names)[:0]
	*values = (*values)[:0]
	// Skip request line.
	rlEnd := bytes.Index(buf, []byte("\r\n"))
	if rlEnd < 0 {
		return 0, false
	}
	pos := rlEnd + 2
	for pos < len(buf) {
		// Empty line = end of headers.
		if pos+1 < len(buf) && buf[pos] == '\r' && buf[pos+1] == '\n' {
			return pos + 2, true
		}
		lineEnd := bytes.Index(buf[pos:], []byte("\r\n"))
		if lineEnd < 0 {
			return 0, false
		}
		line := buf[pos : pos+lineEnd]
		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			return 0, false
		}
		// Canonicalize header name.
		*nameBuf = (*nameBuf)[:0]
		upper := true
		for _, c := range line[:colon] {
			switch {
			case upper && c >= 'a' && c <= 'z':
				*nameBuf = append(*nameBuf, c-('a'-'A'))
			case !upper && c >= 'A' && c <= 'Z':
				*nameBuf = append(*nameBuf, c+('a'-'A'))
			default:
				*nameBuf = append(*nameBuf, c)
			}
			upper = c == '-'
		}
		// Trim leading spaces from value.
		v := line[colon+1:]
		for len(v) > 0 && (v[0] == ' ' || v[0] == '\t') {
			v = v[1:]
		}
		*valBuf = append((*valBuf)[:0], v...)
		*names = append(*names, string(*nameBuf))
		*values = append(*values, string(*valBuf))
		pos += lineEnd + 2
	}
	return pos, true
}

func lookupHeader(names, values []string, want string) string {
	for i, n := range names {
		if n == want {
			return values[i]
		}
	}
	return ""
}

func matchRoute(routes []apacheRoute, path string) apacheRoute {
	for i := range routes {
		r := &routes[i]
		if r.exact {
			if r.prefix == path {
				return *r
			}
		} else if strings.HasPrefix(path, r.prefix) {
			return *r
		}
	}
	// Fallback 404 — last entry should already be the catch-all, but be safe.
	return apacheRoute{prefix: "/", status: 404, contentType: "text/plain", body: "Not Found\n"}
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 500:
		return "Internal Server Error"
	default:
		return "Unknown"
	}
}

func writeTwoDigit(buf *bytes.Buffer, v int) {
	if v < 0 {
		v = 0
	}
	if v < 10 {
		buf.WriteByte('0')
	}
	buf.WriteString(strconv.Itoa(v))
}

func defaultApacheRoutes() []apacheRoute {
	return []apacheRoute{
		{prefix: "/", exact: true, status: 200, contentType: "text/html; charset=utf-8", body: "<html><body><h1>Welcome</h1></body></html>"},
		{prefix: "/index.html", exact: true, status: 200, contentType: "text/html; charset=utf-8", body: "<html><body>Index</body></html>"},
		{prefix: "/about", exact: true, status: 200, contentType: "text/html; charset=utf-8", body: "<html><body>About</body></html>"},
		{prefix: "/contact", exact: true, status: 200, contentType: "text/html; charset=utf-8", body: "<html><body>Contact</body></html>"},
		{prefix: "/login", exact: true, status: 200, contentType: "text/html; charset=utf-8", body: "<html><body>Login</body></html>"},
		{prefix: "/logout", exact: true, status: 302, contentType: "text/plain", body: "redirecting"},
		{prefix: "/api/v1/users", status: 200, contentType: "application/json", body: `{"users":[]}`},
		{prefix: "/api/v1/orders", status: 200, contentType: "application/json", body: `{"orders":[]}`},
		{prefix: "/api/v1/products", status: 200, contentType: "application/json", body: `{"products":[]}`},
		{prefix: "/api/v2", status: 200, contentType: "application/json", body: `{"v":2}`},
		{prefix: "/static/css", status: 200, contentType: "text/css", body: "body{font:14px sans-serif}"},
		{prefix: "/static/js", status: 200, contentType: "application/javascript", body: "console.log('hi');"},
		{prefix: "/static/img", status: 200, contentType: "image/png", body: "PNGDATA"},
		{prefix: "/static", status: 200, contentType: "application/octet-stream", body: "binary"},
		{prefix: "/admin", status: 403, contentType: "text/plain", body: "Forbidden\n"},
		{prefix: "/.git", status: 403, contentType: "text/plain", body: "Forbidden\n"},
		{prefix: "/health", exact: true, status: 200, contentType: "text/plain", body: "OK\n"},
		{prefix: "/metrics", exact: true, status: 200, contentType: "text/plain", body: "# metrics\n"},
		{prefix: "/old", status: 301, contentType: "text/plain", body: "moved"},
		{prefix: "/", status: 404, contentType: "text/plain", body: "Not Found\n"},
	}
}

func buildApacheRequests(n int) [][]byte {
	methods := []string{"GET", "GET", "GET", "POST", "HEAD"}
	paths := []string{
		"/", "/index.html", "/about", "/contact", "/login",
		"/api/v1/users/42", "/api/v1/orders/recent", "/api/v1/products?cat=3",
		"/api/v2/status", "/static/css/main.css", "/static/js/app.js",
		"/static/img/logo.png", "/admin/dashboard", "/.git/config",
		"/health", "/metrics", "/old/page", "/missing/page",
	}
	hosts := []string{"example.com", "api.example.com", "static.example.com"}
	uas := []string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15",
		"curl/8.4.0",
		"Go-http-client/1.1",
		"Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/120.0",
	}
	out := make([][]byte, n)
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		b.Reset()
		method := methods[i%len(methods)]
		path := paths[i%len(paths)]
		host := hosts[i%len(hosts)]
		ua := uas[i%len(uas)]
		fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", method, path)
		fmt.Fprintf(&b, "host: %s\r\n", host)
		fmt.Fprintf(&b, "USER-AGENT: %s\r\n", ua)
		fmt.Fprintf(&b, "accept: text/html,application/xhtml+xml\r\n")
		fmt.Fprintf(&b, "Accept-Encoding: gzip, deflate, br\r\n")
		fmt.Fprintf(&b, "accept-language: en-US,en;q=0.9\r\n")
		fmt.Fprintf(&b, "X-Request-Id: req-%d\r\n", i)
		fmt.Fprintf(&b, "X-Forwarded-For: 203.0.113.%d\r\n", i&0xff)
		if method == "POST" {
			body := fmt.Sprintf(`{"i":%d}`, i)
			fmt.Fprintf(&b, "Content-Type: application/json\r\n")
			fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n", len(body))
			b.WriteString(body)
		} else {
			b.WriteString("\r\n")
		}
		out[i] = append([]byte(nil), b.Bytes()...)
	}
	return out
}

var _ runner.Benchmark = (*ApacheLike)(nil)

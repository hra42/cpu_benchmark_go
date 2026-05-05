package benchmarks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html"
	"sort"
	"strconv"

	"cpu_bench_go/runner"
)

const (
	PHPSmall  = 50
	PHPMedium = 500
	PHPLarge  = 5_000
)

type phpRequest struct {
	UserID   int64             `json:"user_id"`
	Action   string            `json:"action"`
	Path     string            `json:"path"`
	Locale   string            `json:"locale"`
	Token    string            `json:"token"`
	Payload  map[string]string `json:"payload"`
	Tags     []string          `json:"tags"`
	Quantity int               `json:"quantity"`
}

type phpResponse struct {
	OK      bool              `json:"ok"`
	UserID  int64             `json:"user_id"`
	Action  string            `json:"action"`
	Path    string            `json:"path"`
	HTML    string            `json:"html"`
	Headers map[string]string `json:"headers"`
	Echo    map[string]string `json:"echo"`
}

type PHPLike struct {
	n        int
	requests [][]byte
	hmacKey  []byte
	expected uint64
	dec      phpRequest
	resp     phpResponse
	htmlBuf  bytes.Buffer
	jsonBuf  bytes.Buffer
	scratch  map[string]string
	respHdrs map[string]string
	keyBuf   []string
}

func (p *PHPLike) Name() string { return "php_like" }

func (p *PHPLike) Tags() []string { return []string{"string", "memory", "hashing", "allocation"} }

func (p *PHPLike) Setup(n int) {
	if n <= 0 {
		n = PHPMedium
	}
	p.n = n
	p.hmacKey = []byte("php-bench-session-key-v1")
	p.requests = buildPHPRequests(n)
	p.scratch = make(map[string]string, 32)
	p.respHdrs = make(map[string]string, 8)
	p.keyBuf = make([]string, 0, 32)
	p.htmlBuf.Grow(2048)
	p.jsonBuf.Grow(2048)
	p.expected = p.computeChecksum()
}

func (p *PHPLike) Run() any {
	h := fnv.New64a()
	mac := hmac.New(sha256.New, p.hmacKey)
	for _, body := range p.requests {
		// 1. Parse JSON request body.
		p.dec = phpRequest{}
		if err := json.Unmarshal(body, &p.dec); err != nil {
			return uint64(0)
		}

		// 2. Associative-array work: build a working map from request payload + extras.
		for k := range p.scratch {
			delete(p.scratch, k)
		}
		for k, v := range p.dec.Payload {
			p.scratch[k] = v
		}
		p.scratch["action"] = p.dec.Action
		p.scratch["path"] = p.dec.Path
		p.scratch["locale"] = p.dec.Locale
		p.scratch["user"] = strconv.FormatInt(p.dec.UserID, 10)
		p.scratch["qty"] = strconv.Itoa(p.dec.Quantity)
		for i, tag := range p.dec.Tags {
			p.scratch["tag_"+strconv.Itoa(i)] = tag
		}
		// A few lookups + writes (mirrors PHP array hot path).
		if v, ok := p.scratch["locale"]; ok {
			p.scratch["lang"] = v[:2]
		}
		if v, ok := p.scratch["action"]; ok {
			p.scratch["action_upper"] = v + "_OK"
		}

		// 3. HMAC-SHA256 over token + body (CSRF/session check).
		mac.Reset()
		mac.Write([]byte(p.dec.Token))
		mac.Write(body)
		sigSum := mac.Sum(nil)
		sigHex := make([]byte, 16)
		const hexChars = "0123456789abcdef"
		for i := 0; i < 8; i++ {
			sigHex[i*2] = hexChars[sigSum[i]>>4]
			sigHex[i*2+1] = hexChars[sigSum[i]&0x0f]
		}

		// 4. Render HTML response with escape + interpolation.
		p.htmlBuf.Reset()
		p.htmlBuf.WriteString("<!doctype html><html lang=\"")
		p.htmlBuf.WriteString(html.EscapeString(p.dec.Locale))
		p.htmlBuf.WriteString("\"><head><title>")
		p.htmlBuf.WriteString(html.EscapeString(p.dec.Action))
		p.htmlBuf.WriteString("</title></head><body><h1>Hello, user ")
		p.htmlBuf.WriteString(strconv.FormatInt(p.dec.UserID, 10))
		p.htmlBuf.WriteString("</h1><p>Path: ")
		p.htmlBuf.WriteString(html.EscapeString(p.dec.Path))
		p.htmlBuf.WriteString("</p><ul>")
		for _, tag := range p.dec.Tags {
			p.htmlBuf.WriteString("<li>")
			p.htmlBuf.WriteString(html.EscapeString(tag))
			p.htmlBuf.WriteString("</li>")
		}
		p.htmlBuf.WriteString("</ul><table>")
		p.keyBuf = p.keyBuf[:0]
		for k := range p.scratch {
			p.keyBuf = append(p.keyBuf, k)
		}
		sort.Strings(p.keyBuf)
		for _, k := range p.keyBuf {
			p.htmlBuf.WriteString("<tr><th>")
			p.htmlBuf.WriteString(html.EscapeString(k))
			p.htmlBuf.WriteString("</th><td>")
			p.htmlBuf.WriteString(html.EscapeString(p.scratch[k]))
			p.htmlBuf.WriteString("</td></tr>")
		}
		p.htmlBuf.WriteString("</table></body></html>")

		// 5. Build response struct + JSON-encode.
		for k := range p.respHdrs {
			delete(p.respHdrs, k)
		}
		p.respHdrs["Content-Type"] = "text/html; charset=utf-8"
		p.respHdrs["X-Session-Sig"] = string(sigHex)
		p.respHdrs["X-Locale"] = p.dec.Locale
		p.respHdrs["X-User"] = strconv.FormatInt(p.dec.UserID, 10)

		p.resp.OK = true
		p.resp.UserID = p.dec.UserID
		p.resp.Action = p.dec.Action
		p.resp.Path = p.dec.Path
		p.resp.HTML = p.htmlBuf.String()
		p.resp.Headers = p.respHdrs
		p.resp.Echo = p.scratch

		p.jsonBuf.Reset()
		enc := json.NewEncoder(&p.jsonBuf)
		if err := enc.Encode(&p.resp); err != nil {
			return uint64(0)
		}

		// 6. Fold both rendered outputs into running checksum.
		h.Write(p.htmlBuf.Bytes())
		h.Write(p.jsonBuf.Bytes())
		h.Write(sigSum)
	}
	return h.Sum64()
}

func (p *PHPLike) Verify(result any) bool {
	got, ok := result.(uint64)
	if !ok {
		return false
	}
	return got == p.expected
}

func (p *PHPLike) computeChecksum() uint64 {
	v, _ := p.Run().(uint64)
	return v
}

func buildPHPRequests(n int) [][]byte {
	actions := []string{"login", "view", "purchase", "search", "update", "logout"}
	locales := []string{"en-US", "de-DE", "fr-FR", "es-ES", "ja-JP"}
	paths := []string{"/", "/products", "/cart", "/account", "/orders/42", "/search?q=widgets"}
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		req := phpRequest{
			UserID: int64(1000 + i),
			Action: actions[i%len(actions)],
			Path:   paths[i%len(paths)],
			Locale: locales[i%len(locales)],
			Token:  fmt.Sprintf("tok_%d_%x", i, i*2654435761),
			Payload: map[string]string{
				"sku":      fmt.Sprintf("SKU-%05d", i%9999),
				"campaign": fmt.Sprintf("camp-%d", i%17),
				"referrer": fmt.Sprintf("ref-%d", i%23),
				"variant":  fmt.Sprintf("v%d", i%7),
			},
			Tags:     []string{"web", actions[i%len(actions)], fmt.Sprintf("seg%d", i%11)},
			Quantity: 1 + i%5,
		}
		buf, err := json.Marshal(&req)
		if err != nil {
			panic(err)
		}
		out[i] = buf
	}
	return out
}

var _ runner.Benchmark = (*PHPLike)(nil)

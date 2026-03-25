package preview

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxCacheSize = 1000
	cacheTTL     = 1 * time.Hour
	fetchTimeout = 5 * time.Second
	maxBodySize  = 1 * 1024 * 1024 // 1MB
)

type Result struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	URL         string `json:"url"`
}

type cacheEntry struct {
	result    *Result
	expiresAt time.Time
}

type inflightCall struct {
	wg  sync.WaitGroup
	res *Result
	err error
}

type Previewer struct {
	client   *http.Client
	cache    map[string]*cacheEntry
	inflight map[string]*inflightCall
	mu       sync.RWMutex
}

func New() *Previewer {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		// Validate the resolved IP at dial time to prevent DNS rebinding attacks.
		// A hostname check before Do() is racy; by the time the TCP connection
		// is made, DNS could have changed to point at a private address.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no addresses for %s", host)
			}
			ip := ips[0]
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("private IP blocked")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		},
		TLSHandshakeTimeout: 3 * time.Second,
	}

	return &Previewer{
		client: &http.Client{
			Timeout:   fetchTimeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				// Only check literal IPs here — do NOT do a DNS lookup.
				// A second lookup creates a TOCTOU window for DNS rebinding;
				// DialContext already validates the resolved IP at connection time.
				if ip := net.ParseIP(req.URL.Hostname()); ip != nil && isPrivateNetIP(ip) {
					return fmt.Errorf("redirect to private IP blocked")
				}
				return nil
			},
		},
		cache:    make(map[string]*cacheEntry),
		inflight: make(map[string]*inflightCall),
	}
}

func (p *Previewer) Fetch(rawURL string) (*Result, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme")
	}

	// Check cache
	p.mu.RLock()
	if entry, ok := p.cache[rawURL]; ok && time.Now().Before(entry.expiresAt) {
		p.mu.RUnlock()
		return entry.result, nil
	}
	p.mu.RUnlock()

	// Deduplicate concurrent fetches for the same URL
	p.mu.Lock()
	if call, ok := p.inflight[rawURL]; ok {
		p.mu.Unlock()
		call.wg.Wait()
		return call.res, call.err
	}
	call := &inflightCall{}
	call.wg.Add(1)
	p.inflight[rawURL] = call
	p.mu.Unlock()

	defer func() {
		call.wg.Done()
		p.mu.Lock()
		delete(p.inflight, rawURL)
		p.mu.Unlock()
	}()

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		call.err = err
		return nil, err
	}
	req.Header.Set("User-Agent", "BeamBot/1.0 (link preview)")
	req.Header.Set("Accept", "text/html")

	resp, err := p.client.Do(req)
	if err != nil {
		call.err = err
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Cache negative results with a short TTL so repeated requests for the
		// same unavailable URL don't hammer the remote server on every preview.
		p.mu.Lock()
		p.cache[rawURL] = &cacheEntry{
			result:    &Result{URL: rawURL},
			expiresAt: time.Now().Add(5 * time.Minute),
		}
		p.mu.Unlock()
		call.err = fmt.Errorf("status %d", resp.StatusCode)
		return nil, call.err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		call.err = err
		return nil, err
	}

	// Normalize whitespace: collapse newlines to spaces so the OG-tag regexes
	// match attributes whose values span multiple lines in minified/pretty HTML.
	html := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(string(body))
	result := &Result{
		URL:         rawURL,
		Title:       extractMeta(html, "og:title"),
		Description: extractMeta(html, "og:description"),
		Image:       extractMeta(html, "og:image"),
	}

	if result.Title == "" {
		result.Title = extractTitle(html)
	}
	if result.Description == "" {
		result.Description = extractMeta(html, "description")
	}

	// Resolve relative image URLs to absolute using the page URL as base
	if result.Image != "" {
		imgParsed, err := url.Parse(result.Image)
		if err == nil && !imgParsed.IsAbs() {
			result.Image = parsed.ResolveReference(imgParsed).String()
		}
	}

	// Cache result
	p.mu.Lock()
	if len(p.cache) >= maxCacheSize {
		p.evictExpired()
		if len(p.cache) >= maxCacheSize {
			p.evictOldest()
		}
	}
	p.cache[rawURL] = &cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(cacheTTL),
	}
	p.mu.Unlock()

	call.res = result
	return result, nil
}

func (p *Previewer) evictExpired() {
	now := time.Now()
	for key, entry := range p.cache {
		if !now.Before(entry.expiresAt) {
			delete(p.cache, key)
		}
	}
}

func (p *Previewer) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, entry := range p.cache {
		if first || entry.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.expiresAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(p.cache, oldestKey)
	}
}

var (
	ogTagRe    = regexp.MustCompile(`(?i)<meta\s+(?:[^>]*?property\s*=\s*["']([^"']+)["'][^>]*?content\s*=\s*["']([^"']+)["']|[^>]*?content\s*=\s*["']([^"']+)["'][^>]*?property\s*=\s*["']([^"']+)["'])`)
	metaNameRe = regexp.MustCompile(`(?i)<meta\s+(?:[^>]*?name\s*=\s*["']([^"']+)["'][^>]*?content\s*=\s*["']([^"']+)["']|[^>]*?content\s*=\s*["']([^"']+)["'][^>]*?name\s*=\s*["']([^"']+)["'])`)
	titleRe    = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
)

func extractMeta(html, property string) string {
	// Try OG property first
	matches := ogTagRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		prop := m[1]
		content := m[2]
		if prop == "" {
			prop = m[4]
			content = m[3]
		}
		if strings.EqualFold(prop, property) {
			return strings.TrimSpace(content)
		}
	}

	// Try meta name
	matches = metaNameRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		name := m[1]
		content := m[2]
		if name == "" {
			name = m[4]
			content = m[3]
		}
		if strings.EqualFold(name, property) {
			return strings.TrimSpace(content)
		}
	}

	return ""
}

func extractTitle(html string) string {
	m := titleRe.FindStringSubmatch(html)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		nets = append(nets, n)
	}
	return nets
}()

// isPrivateNetIP reports whether ip is in a private/link-local range.
func isPrivateNetIP(ip net.IP) bool {
	for _, network := range privateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// isPrivateIP checks a host string. If the host is not a literal IP it is
// resolved via DNS — only use this path in DialContext (not in CheckRedirect,
// where a second lookup creates a TOCTOU window).
func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return true // block on resolve failure
		}
		ip = ips[0]
	}
	return isPrivateNetIP(ip)
}

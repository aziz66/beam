package preview

import (
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

type Previewer struct {
	client *http.Client
	cache  map[string]*cacheEntry
	mu     sync.RWMutex
}

func New() *Previewer {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 3 * time.Second,
		}).DialContext,
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
				if isPrivateIP(req.URL.Hostname()) {
					return fmt.Errorf("redirect to private IP blocked")
				}
				return nil
			},
		},
		cache: make(map[string]*cacheEntry),
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

	if isPrivateIP(parsed.Hostname()) {
		return nil, fmt.Errorf("private IP blocked")
	}

	// Check cache
	p.mu.RLock()
	if entry, ok := p.cache[rawURL]; ok && time.Now().Before(entry.expiresAt) {
		p.mu.RUnlock()
		return entry.result, nil
	}
	p.mu.RUnlock()

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BeamBot/1.0 (link preview)")
	req.Header.Set("Accept", "text/html")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, err
	}

	html := string(body)
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

func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		// Try resolving hostname
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return true // block on resolve failure
		}
		ip = ips[0]
	}

	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}

	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

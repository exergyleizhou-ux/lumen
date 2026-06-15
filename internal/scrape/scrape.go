// Package scrape provides HTML content extraction, link harvesting,
// metadata extraction (OpenGraph, Twitter Cards), and readability-
// style main content identification.
package scrape

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Page struct {
	URL         string
	Title       string
	Description string
	Body        string
	Links       []string
	Images      []string
	Meta        map[string]string
}
type Extractor struct{ mu sync.Mutex }

func NewExtractor() *Extractor { return &Extractor{} }
func (e *Extractor) Extract(html string, baseURL string) *Page {
	p := &Page{URL: baseURL, Meta: map[string]string{}}
	p.Title = e.extractBetween(html, "<title>", "</title>")
	p.Description = e.extractMeta(html, "description")
	p.Links = e.extractLinks(html, baseURL)
	p.Images = e.extractImages(html, baseURL)
	p.Meta["og:title"] = e.extractMeta(html, "og:title")
	p.Meta["og:description"] = e.extractMeta(html, "og:description")
	p.Meta["og:image"] = e.extractMeta(html, "og:image")
	p.Meta["twitter:card"] = e.extractMeta(html, "twitter:card")
	p.Body = e.extractBody(html)
	return p
}
func (e *Extractor) extractBetween(html, start, end string) string {
	si := strings.Index(strings.ToLower(html), strings.ToLower(start))
	if si < 0 {
		return ""
	}
	si += len(start)
	ei := strings.Index(strings.ToLower(html[si:]), strings.ToLower(end))
	if ei < 0 {
		return ""
	}
	return strings.TrimSpace(html[si : si+ei])
}
func (e *Extractor) extractMeta(html, name string) string {
	patterns := []string{
		fmt.Sprintf(`<meta[^>]+property="%s"[^>]+content="([^"]*)"`, name),
		fmt.Sprintf(`<meta[^>]+name="%s"[^>]+content="([^"]*)"`, name),
	}
	for _, pat := range patterns {
		re := regexp.MustCompile(`(?i)` + pat)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}
func (e *Extractor) extractLinks(html, baseURL string) []string {
	re := regexp.MustCompile(`(?i)<a[^>]+href="([^"#][^"]*)"`)
	matches := re.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var links []string
	for _, m := range matches {
		if len(m) > 1 {
			link := e.resolveURL(m[1], baseURL)
			if link != "" && !seen[link] {
				seen[link] = true
				links = append(links, link)
			}
		}
	}
	sort.Strings(links)
	return links
}
func (e *Extractor) extractImages(html, baseURL string) []string {
	re := regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"`)
	matches := re.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var imgs []string
	for _, m := range matches {
		if len(m) > 1 {
			img := e.resolveURL(m[1], baseURL)
			if img != "" && !seen[img] {
				seen[img] = true
				imgs = append(imgs, img)
			}
		}
	}
	return imgs
}
func (e *Extractor) extractBody(html string) string {
	re := regexp.MustCompile(`(?is)<body[^>]*>(.*)</body>`)
	if m := re.FindStringSubmatch(html); len(m) > 1 {
		return stripTags(m[1])
	}
	return stripTags(html)
}
func (e *Extractor) resolveURL(raw, base string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return b.ResolveReference(u).String()
}
func stripTags(html string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return strings.TrimSpace(re.ReplaceAllString(html, ""))
}
func (p *Page) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Page: %s\n%s\n\n", p.URL, strings.Repeat("─", 50))
	if p.Title != "" {
		fmt.Fprintf(&sb, "Title: %s\n", p.Title)
	}
	if p.Description != "" {
		fmt.Fprintf(&sb, "Summary: %s\n", p.Description)
	}
	fmt.Fprintf(&sb, "Links: %d\n", len(p.Links))
	fmt.Fprintf(&sb, "Images: %d\n", len(p.Images))
	if len(p.Meta) > 0 {
		fmt.Fprintf(&sb, "\nMeta:\n")
		for k, v := range p.Meta {
			if v != "" {
				fmt.Fprintf(&sb, "  %s: %s\n", k, v)
			}
		}
	}
	if len(p.Body) > 0 {
		body := p.Body
		if len(body) > 200 {
			body = body[:200] + "..."
		}
		fmt.Fprintf(&sb, "\nBody preview: %s\n", body)
	}
	return sb.String()
}

package fetch

import (
	"html"
	"regexp"
	"strings"
)

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]*>`)
	wsRe          = regexp.MustCompile(`[ \t]+`)
	blankLinesRe  = regexp.MustCompile(`\n\s*\n+`)

	titleRe       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTagRe     = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attrNameRe    = regexp.MustCompile(`(?is)\b(?:name|property)\s*=\s*["']([^"']+)["']`)
	attrContentRe = regexp.MustCompile(`(?is)\bcontent\s*=\s*["']([^"']*)["']`)
	jsonLDRe      = regexp.MustCompile(`(?is)<script[^>]+type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)
)

// wantMeta are the <meta> name/property keys that carry job-posting signal even
// when the visible body is rendered client-side.
var wantMeta = map[string]bool{
	"description":         true,
	"og:title":            true,
	"og:description":      true,
	"og:site_name":        true,
	"twitter:title":       true,
	"twitter:description": true,
}

// ExtractStructured pulls the high-signal, machine-readable parts of a page that
// HTMLToText discards: the <title>, key <meta>/OpenGraph tags, and any JSON-LD
// blocks describing a JobPosting. Modern job boards (Glints, Dealls, Lever,
// Greenhouse, LinkedIn) render the body with JavaScript but still emit these for
// SEO, so this is often the only reliable place to read the role from. Returns a
// compact text block (capped) suitable to prepend to the body text for the LLM.
func ExtractStructured(htmlBytes []byte) string {
	s := string(htmlBytes)
	var b strings.Builder

	if m := titleRe.FindStringSubmatch(s); m != nil {
		if t := cleanInline(m[1]); t != "" {
			b.WriteString("Title: " + t + "\n")
		}
	}

	for _, tag := range metaTagRe.FindAllString(s, -1) {
		name := attrNameRe.FindStringSubmatch(tag)
		content := attrContentRe.FindStringSubmatch(tag)
		if name == nil || content == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(name[1]))
		if !wantMeta[key] {
			continue
		}
		if v := cleanInline(content[1]); v != "" {
			b.WriteString(key + ": " + v + "\n")
		}
	}

	for _, m := range jsonLDRe.FindAllStringSubmatch(s, -1) {
		block := strings.TrimSpace(m[1])
		// Only keep job-relevant structured data, and cap its size.
		if !strings.Contains(block, "JobPosting") {
			continue
		}
		if len(block) > 8000 {
			block = block[:8000]
		}
		b.WriteString("\nJSON-LD JobPosting:\n" + html.UnescapeString(block) + "\n")
	}

	return strings.TrimSpace(b.String())
}

// cleanInline unescapes entities and collapses whitespace in a short attribute.
func cleanInline(s string) string {
	s = html.UnescapeString(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

// HTMLToText reduces an HTML page to readable text suitable for feeding an LLM:
// it drops script/style blocks, strips tags, unescapes entities, and collapses
// whitespace. The result is truncated to maxChars (0 = no limit). This is
// lossy by design — enough for extraction, not a faithful render.
func HTMLToText(htmlBytes []byte, maxChars int) string {
	s := string(htmlBytes)
	s = scriptStyleRe.ReplaceAllString(s, " ")
	// Turn block boundaries into newlines so structure survives tag removal.
	s = strings.NewReplacer(
		"</p>", "\n", "<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</div>", "\n", "</li>", "\n", "</h1>", "\n", "</h2>", "\n",
		"</h3>", "\n", "</tr>", "\n",
	).Replace(s)
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	s = wsRe.ReplaceAllString(s, " ")
	s = blankLinesRe.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)

	if maxChars > 0 && len(s) > maxChars {
		s = s[:maxChars]
	}
	return s
}

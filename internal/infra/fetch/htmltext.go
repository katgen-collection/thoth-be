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
)

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

package ui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2/compat"
	"github.com/hugoh/gh-bulk-pr/internal/github"
)

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

type rendererKey struct {
	width int
	dark  bool
}

type bodyKey struct {
	rendererKey

	id, body string
}

// markdownCache renders PR bodies as Markdown once per body, width and
// background. It lives behind a pointer so the value-receiver View can fill it.
type markdownCache struct {
	renderers map[rendererKey]*glamour.TermRenderer
	bodies    map[bodyKey]string
	renders   int
}

func newMarkdownCache() *markdownCache {
	return &markdownCache{
		renderers: map[rendererKey]*glamour.TermRenderer{},
		bodies:    map[bodyKey]string{},
	}
}

// body returns item's body rendered to width cells and cut to maxLines lines.
// A body that can't be rendered is shown as it is.
func (c *markdownCache) body(item github.PR, width, maxLines int) string {
	key := bodyKey{
		id:          item.ID,
		body:        item.Body,
		rendererKey: rendererKey{width, compat.HasDarkBackground},
	}

	rendered, ok := c.bodies[key]
	if !ok {
		rendered = c.render(item.Body, key.rendererKey)
		c.bodies[key] = rendered
	}

	return cutLines(rendered, maxLines)
}

func (c *markdownCache) render(body string, key rendererKey) string {
	body = strings.TrimSpace(htmlComment.ReplaceAllString(body, ""))
	if body == "" {
		return ""
	}

	c.renders++

	renderer, err := c.renderer(key)
	if err != nil {
		return body
	}

	out, err := renderer.Render(body)
	if err != nil {
		return body
	}

	return strings.Trim(out, "\n")
}

func (c *markdownCache) renderer(key rendererKey) (*glamour.TermRenderer, error) {
	if renderer, ok := c.renderers[key]; ok {
		return renderer, nil
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle(key.dark)),
		glamour.WithWordWrap(key.width),
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // only used to fall back to the plain body
	}

	c.renderers[key] = renderer

	return renderer, nil
}

// markdownStyle is Glamour's light or dark style without the document margin,
// which the preview box already provides.
func markdownStyle(dark bool) ansi.StyleConfig {
	style := styles.LightStyleConfig
	if dark {
		style = styles.DarkStyleConfig
	}

	noMargin := uint(0)
	style.Document.Margin = &noMargin
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""

	return style
}

// cutLines keeps the first maxLines lines of s, the last of them an ellipsis
// when something was cut.
func cutLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}

	return strings.Join(append(lines[:max(maxLines-1, 0)], "…"), "\n")
}

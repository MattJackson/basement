package docs

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/extension"
)

//go:embed *.md
var docsFS embed.FS

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Strikethrough,
		extension.TaskList,
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
	),
)

const chromeTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title>
<style>
:root{--bg:#0a0a0b;--fg:#e5e5e7;--muted:#8a8a93;--accent:#6366f1;--border:#2a2a2e}
*{box-sizing:border-box}body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:var(--bg);color:var(--fg);line-height:1.6}
nav{background:linear-gradient(180deg,rgba(99,102,241,.15),transparent);border-bottom:1px solid var(--border);padding:.75rem 1.5rem;font-size:.875rem}
nav a{color:var(--accent);text-decoration:none;margin-right:1rem}nav a:hover{text-decoration:underline}
main{max-width:720px;margin:0 auto;padding:2rem 1rem}h1,h2,h3{margin-top:1.5rem;margin-bottom:.75rem;line-height:1.25}p,ul,ol{margin-bottom:1rem}a{color:var(--accent)}code{background:#1a1a1e;padding:.15em .3em;border-radius:4px;font-size:.875em}pre{background:#1a1a1e;padding:1rem;border-radius:6px;overflow-x:auto}pre code{background:none;padding:0}blockquote{border-left:3px solid var(--accent);margin:0;padding-left:1rem;color:var(--muted)}
@media(max-width:640px){main{padding:.75rem 1rem}}
</style>
</head>
<body><nav><a href="/">Basement</a><a href="/docs/">Docs</a></nav><main>%s</main></body></html>`

func RenderMarkdown(filename string) (string, error) {
	data, err := docsFS.ReadFile(filename + ".md")
	if err != nil {
		return "", fmt.Errorf("reading %s.md: %w", filename, err)
	}

	var buf strings.Builder
	if err := md.Convert(data, &buf); err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	title := strings.Title(filepath.Base(filename))

	return fmt.Sprintf(chromeTemplate, title, buf.String()), nil
}

func HandleDocs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/docs/")
	if path == "" || path == "/" {
		http.Redirect(w, r, "/docs/integrations", http.StatusSeeOther)
		return
	}

	basePath := strings.TrimSuffix(path, "/")
	possiblePaths := []string{
		basePath + ".md",
		strings.TrimSuffix(basePath, ".md") + ".md",
		filepath.Join(basePath, "index.md"),
	}

	var rendered string
	var err error
	for _, p := range possiblePaths {
		rendered, err = RenderMarkdown(p)
		if err == nil {
			break
		}
	}

	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "reading") {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, time.Now().UnixNano()))
	fmt.Fprint(w, rendered)
}

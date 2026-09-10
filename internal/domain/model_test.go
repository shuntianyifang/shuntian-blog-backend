package domain

import (
	"strings"
	"testing"
	"time"
)

func TestRenderAndValidate(t *testing.T) {
	html, err := Render("# Hello\n\n<script>alert('x')</script>\n\n[bad](javascript:alert(1))\n\n```go\nfmt.Println(1)\n```\n")
	if err != nil || strings.Contains(html, "<script") || strings.Contains(html, "javascript:") || !strings.Contains(html, "fmt.Println(1)") {
		t.Fatal("unsafe or missing rendering")
	}
	d := Draft{Slug: "hello", Title: "Hello", PublishedAt: time.Now(), Category: Taxon{Slug: "notes", Name: "Notes"}}
	if Validate(d) != nil {
		t.Fatal("valid draft rejected")
	}
	d.Slug = "../admin"
	if Validate(d) == nil {
		t.Fatal("unsafe slug accepted")
	}
}

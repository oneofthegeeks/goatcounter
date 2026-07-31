package goatcounter

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// themedCSS are the stylesheets that consume theme tokens.
var themedCSS = []string{"backend.css", "shared.css", "style.css", "i18n.css"}

// Every var(--x) used in the CSS must be defined in BOTH vars.css (light) and
// dark.css (dark). A token defined in only one file silently keeps the other
// theme's value, which is the classic way a retheme ends up half-applied.
func TestThemeTokensDefinedInBothFiles(t *testing.T) {
	used := make(map[string]bool)
	useRe := regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)
	for _, f := range themedCSS {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range useRe.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
	}
	if len(used) == 0 {
		t.Fatal("found no var(--x) usages; the test is not reading the right files")
	}

	defRe := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:`)
	for _, f := range []string{"vars.css", "dark.css"} {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		defined := make(map[string]bool)
		for _, m := range defRe.FindAllStringSubmatch(string(b), -1) {
			defined[m[1]] = true
		}

		var missing []string
		for tok := range used {
			if !defined[tok] {
				missing = append(missing, tok)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("public/%s does not define %d token(s) the CSS uses: %s",
				f, len(missing), strings.Join(missing, ", "))
		}
	}
}

// Colours belong in vars.css and dark.css, never inline in a stylesheet, or
// they survive the theme switch and look wrong on the opposite background.
func TestNoHardcodedColours(t *testing.T) {
	comments := regexp.MustCompile(`(?s)/\*.*?\*/`)
	// A hex colour in a declaration value: after the ':' of a property.
	value := regexp.MustCompile(`:[^;{}]*`)
	hex := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

	for _, f := range []string{"backend.css", "shared.css"} {
		b, err := os.ReadFile("public/" + f)
		if err != nil {
			t.Fatal(err)
		}
		src := comments.ReplaceAllString(string(b), "")

		var found []string
		for _, decl := range value.FindAllString(src, -1) {
			found = append(found, hex.FindAllString(decl, -1)...)
		}
		if len(found) > 0 {
			t.Errorf("public/%s has %d hardcoded colour(s), move them to vars.css and dark.css: %s",
				f, len(found), strings.Join(found, ", "))
		}
	}
}

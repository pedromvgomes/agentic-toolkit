package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// The language table is data, not logic: statement coverage of the loops that
// read it is satisfied by exercising one language, so a wrong pattern for any
// other is invisible without a case naming that language.
func TestExportedSymbolsPerLanguage(t *testing.T) {
	cases := []struct {
		lang review.Language
		line string
		want string
	}{
		{review.LangGo, "func Widget() error {", "Widget"},
		{review.LangGo, "type Config struct {", "Config"},
		{review.LangRust, "pub fn widget(&self) -> u32 {", "widget"},
		{review.LangRust, "pub struct Config {", "Config"},
		{review.LangPython, "def widget(self):", "widget"},
		{review.LangPython, "class Config:", "Config"},
		{review.LangTypeScript, "export function widget(): void {", "widget"},
		{review.LangTypeScript, "export interface Config {", "Config"},
		{review.LangJavaScript, "export const widget = 1", "widget"},
		{review.LangKotlin, "class ConfigLoader(private val x: Int) {", "ConfigLoader"},
		{review.LangJava, "public final class ConfigLoader {", "ConfigLoader"},
		{review.LangRuby, "  def widget", "widget"},
		{review.LangRuby, "class Config", "Config"},
		{review.LangCSharp, "    public sealed class ConfigLoader {", "ConfigLoader"},
		{review.LangPHP, "    public function widget() {", "widget"},
		{review.LangPHP, "final class Config {", "Config"},
		{review.LangSwift, "public func widget() {", "widget"},
		{review.LangScala, "case class Config(x: Int)", "Config"},
		{review.LangCPP, "class ConfigLoader {", "ConfigLoader"},
	}

	for _, tc := range cases {
		t.Run(string(tc.lang)+"/"+tc.want, func(t *testing.T) {
			got := review.ExportedSymbols(tc.lang, tc.line)
			for _, name := range got {
				if name == tc.want {
					return
				}
			}
			t.Errorf("ExportedSymbols(%s, %q) = %v, want it to include %q", tc.lang, tc.line, got, tc.want)
		})
	}
}

// A language with an extractor contributes symbols; a format that exports
// nothing callable contributes zero, which is an answer; anything else makes
// the count unavailable rather than low.
func TestSymbolsCountableByLanguage(t *testing.T) {
	cases := []struct {
		lang review.Language
		want bool
	}{
		{review.LangGo, true},
		{review.LangRust, true},
		{review.LangKotlin, true},
		{review.LangSQL, true},
		{review.LangYAML, true},
		{review.LangTerraform, true},
		{review.LangDocs, true},
		{review.LangUnknown, false},
	}
	for _, tc := range cases {
		if got := review.SymbolsCountable(tc.lang); got != tc.want {
			t.Errorf("SymbolsCountable(%q) = %v, want %v", tc.lang, got, tc.want)
		}
	}
}

// A comment is prose; a signal is a claim about what the code does. The
// comment marker differs per language, so each language's needs a case.
func TestIsCommentPerLanguage(t *testing.T) {
	cases := []struct {
		lang    review.Language
		line    string
		comment bool
	}{
		{review.LangGo, "// var mu sync.Mutex", true},
		{review.LangGo, "var mu sync.Mutex", false},
		{review.LangPython, "# import threading", true},
		{review.LangPython, "import threading", false},
		{review.LangSQL, "-- ALTER TABLE t ADD COLUMN c", true},
		{review.LangSQL, "ALTER TABLE t ADD COLUMN c", false},
		{review.LangYAML, "# kind: Deployment", true},
		{review.LangTerraform, "# resource \"aws_s3_bucket\" \"b\" {", true},
		{review.LangJava, " * a doc comment continuation", true},
	}
	for _, tc := range cases {
		if got := review.IsComment(tc.lang, tc.line); got != tc.comment {
			t.Errorf("IsComment(%q, %q) = %v, want %v", tc.lang, tc.line, got, tc.comment)
		}
	}
}

func TestLanguageOf(t *testing.T) {
	cases := map[string]review.Language{
		"main.go":          review.LangGo,
		"src/lib.rs":       review.LangRust,
		"app/models.py":    review.LangPython,
		"web/App.tsx":      review.LangTypeScript,
		"web/app.js":       review.LangJavaScript,
		"Service.kt":       review.LangKotlin,
		"Service.java":     review.LangJava,
		"db/0001_init.sql": review.LangSQL,
		"infra/main.tf":    review.LangTerraform,
		"deploy/k8s.yaml":  review.LangYAML,
		"README.md":        review.LangDocs,
		"go.mod":           review.LangDocs,
		"LICENSE":          review.LangDocs,
		"service.zig":      review.LangUnknown,
	}
	for path, want := range cases {
		if got := review.LanguageOf(path); got != want {
			t.Errorf("LanguageOf(%q) = %q, want %q", path, got, want)
		}
	}
}

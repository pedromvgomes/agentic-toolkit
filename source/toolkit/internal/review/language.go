package review

import (
	"path"
	"regexp"
	"strings"
)

// Language is what a changed file is written in, as far as the review engine
// can tell from its name.
//
// It exists to serve two things at once — which content patterns a signal is
// detected with, and how an exported symbol is recognised — because those are
// the same knowledge. Two tables would let a language be known to one and
// unknown to the other, so a change could carry a `concurrency` signal and
// have no extractable symbols for reasons nobody could explain.
type Language string

const (
	// LangUnknown is a file no entry claims. Its lines still count; only the
	// language-specific readings are unavailable.
	LangUnknown    Language = ""
	LangGo         Language = "go"
	LangRust       Language = "rust"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangJavaScript Language = "javascript"
	LangKotlin     Language = "kotlin"
	LangJava       Language = "java"
	LangRuby       Language = "ruby"
	LangCSharp     Language = "csharp"
	LangPHP        Language = "php"
	LangSwift      Language = "swift"
	LangScala      Language = "scala"
	LangCPP        Language = "cpp"
	LangC          Language = "c"
	LangSQL        Language = "sql"
	LangTerraform  Language = "terraform"
	LangShell      Language = "shell"
	LangYAML       Language = "yaml"
	// LangDocs is documentation, data and project metadata: formats that are
	// read rather than called.
	LangDocs Language = "docs"
)

// langSpec is everything the engine knows about one language.
type langSpec struct {
	// Extensions and Filenames are how a path is recognised.
	Extensions []string
	Filenames  []string

	// NoSymbols records that this language does not export symbols other code
	// calls. A YAML manifest or a Markdown page contributes nothing to a
	// blast-radius count, and that is a fact about the format rather than a
	// gap in the toolkit — so it leaves the count available at zero rather
	// than making it unavailable.
	NoSymbols bool

	// Exported matches a line that declares a symbol other code can reach,
	// capturing the name in group 1. A language that could export symbols and
	// has no extractor here makes referencing_files unavailable for a change
	// written in it — never low.
	Exported []*regexp.Regexp

	// LineComment are the prefixes that start a comment in this language. A
	// commented line is prose, and a signal is a claim about what the code
	// does — so detection skips them, and a file explaining why it locks
	// nothing does not report that it locks something.
	LineComment []string

	// Content maps a signal to the patterns that evidence it in this
	// language's source.
	Content map[Signal][]*regexp.Regexp
}

// re compiles a pattern at init. A pattern in this table is a constant, so a
// bad one is a build-time defect rather than a review that silently detects
// nothing.
func re(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

var languages = map[Language]langSpec{
	LangGo: {
		LineComment: []string{"//"},
		Extensions:  []string{".go"},
		Exported: []*regexp.Regexp{
			re(`^func ([A-Z]\w*)`),
			re(`^func \([^)]*\) ([A-Z]\w*)`),
			re(`^type ([A-Z]\w*)`),
			re(`^(?:var|const) ([A-Z]\w*)`),
			re(`^\t([A-Z]\w*)\s+[\w\[\]*.]+\s*(?:` + "`" + `|$)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {
				re(`sync\.(?:Mutex|RWMutex|WaitGroup|Once|Map|Pool)`),
				re(`\bgo func\b`), re(`\bchan\s`), re(`atomic\.`),
				re(`context\.With(?:Cancel|Timeout|Deadline)`),
				re(`\bselect\s*{`), re(`errgroup\.`),
			},
			SignalCrypto: {
				re(`crypto/`), re(`\btls\.Config\b`), re(`x509\.`),
				re(`\brand\.Read\b`), re(`bcrypt\.`), re(`argon2\.`),
			},
			SignalPublicAPI: {re(`json:"`), re(`protobuf:"`), re(`\bhttp\.Handle`)},
		},
	},

	LangRust: {
		LineComment: []string{"//"},
		Extensions:  []string{".rs"},
		Exported: []*regexp.Regexp{
			re(`^\s*pub(?:\([^)]*\))?\s+(?:async\s+)?fn\s+(\w+)`),
			re(`^\s*pub(?:\([^)]*\))?\s+(?:struct|enum|trait|type|const|static|mod)\s+(\w+)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {
				re(`\bArc<`), re(`\bMutex<`), re(`\bRwLock<`), re(`tokio::spawn`),
				re(`\.await\b`), re(`std::sync::`), re(`\bunsafe\b`),
			},
			SignalCrypto: {re(`\bring::`), re(`\brustls\b`), re(`\bsha2::`), re(`\bargon2\b`)},
		},
	},

	LangPython: {
		LineComment: []string{"#"},
		Extensions:  []string{".py"},
		Exported: []*regexp.Regexp{
			re(`^(?:async )?def ([a-zA-Z]\w*)`),
			re(`^class ([a-zA-Z]\w*)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {
				re(`\bthreading\.`), re(`\basyncio\.`), re(`multiprocessing`),
				re(`\basync def\b`), re(`\bawait\b`), re(`\bLock\(\)`),
			},
			SignalCrypto:    {re(`\bhashlib\b`), re(`\bcryptography\b`), re(`\bbcrypt\b`), re(`\bsecrets\.`)},
			SignalPublicAPI: {re(`@app\.(?:route|get|post|put|delete)`), re(`@router\.`)},
		},
	},

	LangTypeScript: {
		Extensions:  []string{".ts", ".tsx"},
		LineComment: []string{"//"},
		Exported:    jsExported,
		Content:     jsContent,
	},
	LangJavaScript: {
		Extensions:  []string{".js", ".jsx", ".mjs", ".cjs"},
		LineComment: []string{"//"},
		Exported:    jsExported,
		Content:     jsContent,
	},

	LangKotlin: {
		LineComment: []string{"//"},
		Extensions:  []string{".kt", ".kts"},
		Exported:    jvmExported,
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {
				re(`\bsynchronized\b`), re(`\bReentrantLock\b`), re(`\bAtomic\w+`),
				re(`\bsuspend fun\b`), re(`\bcoroutineScope\b`), re(`\blaunch\s*{`),
				re(`@Transactional`),
			},
			SignalCrypto:           {re(`\bSecureRandom\b`), re(`\bMessageDigest\b`), re(`\bKeyStore\b`), re(`\bCipher\b`), re(`\bBCrypt\b`)},
			SignalPublicAPI:        {re(`@(?:Get|Post|Put|Delete|Patch|Request)Mapping`), re(`@RestController`)},
			SignalMessageConsumers: {re(`@KafkaListener`), re(`@RabbitListener`), re(`@Scheduled`), re(`@JmsListener`)},
		},
	},
	LangJava: {
		LineComment: []string{"//"},
		Extensions:  []string{".java"},
		Exported:    jvmExported,
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {
				re(`\bsynchronized\b`), re(`\bReentrantLock\b`), re(`\bAtomic\w+`),
				re(`\bCompletableFuture\b`), re(`\bExecutorService\b`), re(`@Transactional`),
			},
			SignalCrypto:           {re(`\bSecureRandom\b`), re(`\bMessageDigest\b`), re(`\bKeyStore\b`), re(`\bCipher\b`)},
			SignalPublicAPI:        {re(`@(?:Get|Post|Put|Delete|Patch|Request)Mapping`), re(`@RestController`)},
			SignalMessageConsumers: {re(`@KafkaListener`), re(`@RabbitListener`), re(`@Scheduled`), re(`@JmsListener`)},
		},
	},

	LangRuby: {
		LineComment: []string{"#"},
		Extensions:  []string{".rb"},
		Exported: []*regexp.Regexp{
			re(`^\s*def ([a-z_]\w*)`),
			re(`^\s*(?:class|module) ([A-Z]\w*)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`\bMutex\b`), re(`\bThread\.new\b`), re(`\btransaction\b`)},
			SignalCrypto:      {re(`\bOpenSSL\b`), re(`\bBCrypt\b`), re(`\bSecureRandom\b`)},
		},
	},

	LangCSharp: {
		LineComment: []string{"//"},
		Extensions:  []string{".cs"},
		Exported: []*regexp.Regexp{
			re(`^\s*public\s+(?:static\s+|abstract\s+|sealed\s+|partial\s+)*(?:class|interface|record|struct|enum)\s+(\w+)`),
			re(`^\s*public\s+(?:static\s+|async\s+|virtual\s+|override\s+)*[\w<>\[\],?]+\s+(\w+)\s*\(`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`\block\s*\(`), re(`\bSemaphoreSlim\b`), re(`\bInterlocked\b`), re(`\basync Task\b`)},
			SignalCrypto:      {re(`\bRandomNumberGenerator\b`), re(`\bAes\b`), re(`\bRSA\b`), re(`\bSHA\d`)},
		},
	},

	LangPHP: {
		LineComment: []string{"//", "#"},
		Extensions:  []string{".php"},
		Exported: []*regexp.Regexp{
			re(`^\s*(?:public\s+)?(?:static\s+)?function (\w+)`),
			re(`^\s*(?:final\s+|abstract\s+)?(?:class|interface|trait|enum) (\w+)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalCrypto: {re(`\bpassword_hash\b`), re(`\bopenssl_`), re(`\brandom_bytes\b`)},
		},
	},

	LangSwift: {
		LineComment: []string{"//"},
		Extensions:  []string{".swift"},
		Exported: []*regexp.Regexp{
			re(`^\s*public\s+(?:func|class|struct|enum|protocol|var|let)\s+(\w+)`),
			re(`^\s*open\s+(?:func|class)\s+(\w+)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`\bDispatchQueue\b`), re(`\bNSLock\b`), re(`\bactor\b`), re(`\bawait\b`)},
			SignalCrypto:      {re(`\bCryptoKit\b`), re(`\bSecRandom`), re(`\bKeychain`)},
		},
	},

	LangScala: {
		LineComment: []string{"//"},
		Extensions:  []string{".scala", ".sc"},
		Exported: []*regexp.Regexp{
			re(`^\s*(?:case )?(?:class|object|trait) (\w+)`),
			re(`^\s*def (\w+)`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`\bFuture\b`), re(`\bIO\b`), re(`\bAtomic\w+`), re(`\bsynchronized\b`)},
		},
	},

	LangCPP: {
		LineComment: []string{"//"},
		Extensions:  []string{".cpp", ".cc", ".cxx", ".hpp", ".hh"},
		Exported: []*regexp.Regexp{
			re(`^\s*(?:class|struct) (\w+)`),
			re(`^[\w:<>*&\s]+\s(\w+)\s*\([^;]*\)\s*(?:const\s*)?{`),
		},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`std::mutex`), re(`std::atomic`), re(`std::thread`), re(`std::lock_guard`)},
			SignalCrypto:      {re(`\bEVP_`), re(`\bOpenSSL\b`), re(`\bRAND_bytes\b`)},
		},
	},
	LangC: {
		LineComment: []string{"//"},
		Extensions:  []string{".c", ".h"},
		Exported:    []*regexp.Regexp{re(`^[\w\s*]+\s\**(\w+)\s*\([^;]*\)\s*{`)},
		Content: map[Signal][]*regexp.Regexp{
			SignalConcurrency: {re(`pthread_`), re(`\batomic_`)},
			SignalCrypto:      {re(`\bEVP_`), re(`\bRAND_bytes\b`)},
		},
	},

	// The remaining entries declare NoSymbols: they are not languages that
	// export symbols other code calls, so the absence of an extractor is a
	// fact about them rather than a gap. They are here because a signal still
	// reads their content, and because a change that touches only these has a
	// blast radius of zero rather than an unknown one.
	LangSQL: {
		LineComment: []string{"--"},
		Extensions:  []string{".sql", ".ddl"},
		NoSymbols:   true,
		Content: map[Signal][]*regexp.Regexp{
			SignalMigrations: {
				re(`(?i)\bALTER TABLE\b`), re(`(?i)\bCREATE TABLE\b`), re(`(?i)\bDROP (?:TABLE|COLUMN)\b`),
				re(`(?i)\bNOT NULL\b`), re(`(?i)\bCREATE (?:UNIQUE )?INDEX\b`),
			},
		},
	},
	LangTerraform: {
		LineComment: []string{"#", "//"},
		Extensions:  []string{".tf", ".tfvars"},
		NoSymbols:   true,
		Content: map[Signal][]*regexp.Regexp{
			SignalIaC: {re(`\bresource\s+"`), re(`\bmodule\s+"`), re(`\bprovider\s+"`)},
		},
	},
	LangShell: {
		LineComment: []string{"#"},
		Extensions:  []string{".sh", ".bash", ".zsh"},
		NoSymbols:   true,
	},
	LangYAML: {
		LineComment: []string{"#"},
		Extensions:  []string{".yaml", ".yml"},
		NoSymbols:   true,
		Content: map[Signal][]*regexp.Regexp{
			SignalIaC: {re(`\bapiVersion:`), re(`\bkind:\s*(?:Deployment|Service|StatefulSet|Ingress|ConfigMap|Secret)\b`)},
		},
	},
}

// Documentation, data and project metadata. Listed by name so that a file
// this build does not recognise at all still makes the count unavailable: the
// direction the table fails in matters more than its completeness, and a
// missing entry produces a refusal naming the file rather than a blast-radius
// count that quietly omitted a language.
var docsAndData = langSpec{NoSymbols: true}

var docsAndDataExtensions = []string{
	".md", ".markdown", ".rst", ".adoc", ".txt",
	".json", ".jsonc", ".toml", ".ini", ".cfg", ".conf", ".properties", ".env",
	".csv", ".tsv", ".xml", ".html", ".htm", ".css", ".scss", ".less",
	".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf",
	".lock", ".sum", ".mod", ".gradle", ".bazel", ".bzl", ".gitignore",
	".editorconfig", ".dockerignore", ".gitattributes", ".patch", ".diff",
}

var docsAndDataFilenames = []string{
	"LICENSE", "LICENCE", "NOTICE", "COPYING", "CODEOWNERS", "AUTHORS",
	"Makefile", "Dockerfile", "Justfile", "Rakefile", "Procfile",
	".gitignore", ".gitattributes", ".editorconfig", ".dockerignore",
	"go.mod", "go.sum",
}

// jsExported and jsContent are shared by TypeScript and JavaScript: one
// dialect's export syntax is the other's, and a table that copied them would
// let the two drift apart for no reason a reader could name.
var jsExported = []*regexp.Regexp{
	re(`^export (?:async )?function \*?(\w+)`),
	re(`^export (?:default )?(?:abstract )?class (\w+)`),
	re(`^export (?:const|let|var|type|interface|enum) (\w+)`),
	re(`^export \{ ([^}]+) \}`),
}

var jsContent = map[Signal][]*regexp.Regexp{
	SignalConcurrency: {
		re(`Promise\.(?:all|allSettled|race)`), re(`\bnew Worker\(`),
		re(`\bAtomics\.`), re(`\bSharedArrayBuffer\b`), re(`\bMutex\b`),
	},
	SignalCrypto: {
		re(`\bcrypto\.(?:randomBytes|subtle|createHash|createHmac)`),
		re(`\bbcrypt\b`), re(`\bargon2\b`), re(`\bjose\b`),
	},
	SignalPublicAPI: {
		re(`\bapp\.(?:get|post|put|delete|patch)\(`), re(`\brouter\.(?:get|post|put|delete|patch)\(`),
	},
}

// jvmExported is Kotlin's and Java's, which agree on the visibility keyword
// that matters here.
var jvmExported = []*regexp.Regexp{
	re(`^\s*(?:public\s+|internal\s+)?(?:final\s+|abstract\s+|open\s+|sealed\s+|data\s+)*(?:class|interface|enum class|enum|record|object)\s+(\w+)`),
	re(`^\s*(?:public\s+|internal\s+)?(?:static\s+|suspend\s+|open\s+|override\s+)*(?:fun|[\w<>\[\],?.]+)\s+(\w+)\s*\(`),
}

// extensionIndex and filenameIndex invert the table once, at init, so
// classifying a path is a map lookup rather than a scan.
var (
	extensionIndex = map[string]Language{}
	filenameIndex  = map[string]Language{}
)

func init() {
	for _, ext := range docsAndDataExtensions {
		extensionIndex[ext] = LangDocs
	}
	for _, name := range docsAndDataFilenames {
		filenameIndex[name] = LangDocs
	}
	languages[LangDocs] = docsAndData

	for lang, spec := range languages {
		for _, ext := range spec.Extensions {
			extensionIndex[ext] = lang
		}
		for _, name := range spec.Filenames {
			filenameIndex[name] = lang
		}
	}
}

// LanguageOf classifies a path. A path no entry claims is LangUnknown, which
// is a fact to report rather than a failure: its lines still count, and only
// the language-specific readings are unavailable.
func LanguageOf(p string) Language {
	if lang, ok := filenameIndex[path.Base(p)]; ok {
		return lang
	}
	if lang, ok := extensionIndex[strings.ToLower(path.Ext(p))]; ok {
		return lang
	}
	return LangUnknown
}

// HasSymbolExtractor reports whether exported symbols can be read out of this
// language.
func HasSymbolExtractor(lang Language) bool {
	return len(languages[lang].Exported) > 0
}

// SymbolsCountable reports whether a change containing this language can be
// counted for blast radius at all.
//
// Three cases, and the middle one is the point. A language with an extractor
// contributes its symbols. A language that exports nothing callable
// contributes zero, which is an answer. Anything else — a language this build
// does not recognise, or one it recognises without knowing how to read symbols
// out of — makes the count unavailable, and a rule reading it is refused.
// Unavailable is never low: an escalation that silently never fires leaves a
// repo believing it has a protection it does not have.
func SymbolsCountable(lang Language) bool {
	spec, known := languages[lang]
	return known && (spec.NoSymbols || len(spec.Exported) > 0)
}

// BearsCode reports whether this file may contain something that runs.
//
// It lets a signal treat a name fragment as evidence on `admin_guard.ts` and
// not on `guard-settings.yaml` — a file named for a thing it configures is
// describing a gate, not being one.
//
// Written as a list of formats that are read rather than a list of languages
// that run, because the two fail in opposite directions and only one of them
// is safe. A signal that fires is a more expensive review; a signal that stays
// silent is a check deleted with nobody looking. Asking "is this a language I
// know?" answers no for Elixir, Dart, Lua and every language nobody has added
// yet, so the protection would be absent precisely where the toolkit's
// knowledge is, and nothing would say so.
//
// `NoSymbols` is the wrong question for the same reason: shell carries it only
// because no extractor reads symbols out of shell, and a shell script runs.
func BearsCode(lang Language) bool {
	switch lang {
	case LangYAML, LangDocs:
		return false
	}
	return true
}

// ExportedSymbols reads the names a line declares that other code can reach.
func ExportedSymbols(lang Language, line string) []string {
	var out []string
	for _, pattern := range languages[lang].Exported {
		m := pattern.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		// An `export { a, b }` list is one match carrying several names.
		for _, name := range strings.Split(m[1], ",") {
			name = strings.TrimSpace(name)
			if i := strings.Index(name, " as "); i >= 0 {
				name = strings.TrimSpace(name[i+4:])
			}
			if isSymbolName(name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// testFilePatterns recognise a file whose symbols nothing outside it calls.
//
// A test declares plenty of exported-looking names — `TestSomething` is
// capitalised in Go, and a fixture class is public in Java — and not one of
// them is reachable from anywhere. Counting them fills the symbol budget with
// names that have exactly one referencing file, which turns a blast-radius
// measure into a count of how many tests a change added.
//
// Tests still count as reviewable files and still carry signals. It is only
// the symbol extraction that skips them.
var testFilePatterns = []string{
	"**/*_test.go",
	"**/*_test.py", "**/test_*.py",
	"**/*.test.ts", "**/*.test.tsx", "**/*.test.js", "**/*.test.jsx",
	"**/*.spec.ts", "**/*.spec.tsx", "**/*.spec.js", "**/*.spec.jsx",
	"**/*Test.java", "**/*Tests.java", "**/*Test.kt", "**/*Tests.kt",
	"**/*Test.cs", "**/*Tests.cs",
	"**/*_spec.rb", "**/*_test.rb",
	"**/*_test.rs", "**/tests/**", "**/test/**", "**/__tests__/**",
}

// IsTestFile reports whether a path is a test, by the conventions each
// ecosystem actually follows.
func IsTestFile(p string) bool { return MatchAnyGlob(testFilePatterns, p) }

// symbolNameRE is what a name has to look like to be worth counting. It is
// also what makes the count safe to shell out for: a name that reaches grep
// is matched against this first, so nothing from a diff somebody else wrote
// is ever interpolated into a command line.
var symbolNameRE = regexp.MustCompile(`^[A-Za-z_]\w*$`)

func isSymbolName(s string) bool { return symbolNameRE.MatchString(s) }

// IsComment reports whether a line is prose rather than code in lang.
//
// It reads only line comments. A block comment's interior needs the parser
// this package deliberately does not have, and a heuristic that guessed at it
// would drop real code the day a string contained the closing token — so the
// rule is the one that can be applied to a single line correctly, and the
// residue is a signal firing on a comment rather than code being missed.
func IsComment(lang Language, line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range languages[lang].LineComment {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	// A doc comment continuation in the block-comment languages.
	return strings.HasPrefix(trimmed, "* ") || trimmed == "*"
}

// contentPatterns returns the patterns evidencing sig in lang, plus the ones
// that hold whatever the language.
func contentPatterns(lang Language, sig Signal) []*regexp.Regexp {
	return append(append([]*regexp.Regexp(nil), languages[lang].Content[sig]...), universalContent[sig]...)
}

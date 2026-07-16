//go:build windows

package ingest

import (
	"strings"
	"unicode"
)

// DocumentProfile classifies a document by structure and domain based on
// fast feature signals (no LLM call, <5ms).
type DocumentProfile struct {
	Format     string  `json:"format"`     // "markdown"|"pdf"|"txt"|"html"
	Structure  string  `json:"structure"`  // "highly_structured"|"semi_structured"|"unstructured"
	Domain     string  `json:"domain"`     // "legal"|"technical"|"narrative"|"general"
	Confidence float64 `json:"confidence"` // 0.0-1.0
}

// QuickClassify returns a DocumentProfile based on fast feature signals.
// It never calls an LLM — Layer 2 (LLM confirmation) runs separately during
// structure extraction and overrides these results when they conflict.
func QuickClassify(text string, ext string) DocumentProfile {
	p := DocumentProfile{
		Format:    formatFromExt(ext),
		Structure: "semi_structured",
		Domain:    "general",
		Confidence: 0.5,
	}

	lines := strings.Split(text, "\n")
	totalLines := len(lines)
	if totalLines == 0 {
		return p
	}

	// ── Feature counters ──
	var (
		hierCount      int // Chinese: 第X章/条/节; English: Section/Article/Chapter/Clause/Part
		defCount       int // "本法所称" / "shall mean" / "定义" / "术语解释"
		refCount       int // "第X条" / "§" / "pursuant to section" / "参见"
		dialogCount    int // dialogue markers: ""「」『』
		timeWordCount  int // "昨天" / "then" / "后来" / "three days later"
		numberedCount  int // "1. " / "(a)" / "1.2.3" style numbering
		codeCount      int // func / class / def / import / const / let
		shortLineCount int // lines ≤ 40 runes
	)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) <= 40 {
			shortLineCount++
		}

		// Hierarchy signals
		if matchAnyPattern(line, hierarchyPatterns) {
			hierCount++
		}
		if matchAnyPattern(line, definitionPatterns) {
			defCount++
		}
		if matchAnyPattern(line, referencePatterns) {
			refCount++
		}

		// Numbered list patterns
		if matchAnyPattern(line, numberedPatterns) {
			numberedCount++
		}

		// Dialogue markers
		for _, r := range runes {
			if r == '"' || r == '「' || r == '」' || r == '『' || r == '』' || r == '「' || r == '」' || r == '『' || r == '』' {
				dialogCount++
				break
			}
		}

		// Temporal words
		if matchAnyPattern(line, temporalPatterns) {
			timeWordCount++
		}
	}

	// Code keywords
	codeCount = countMatches(text, codePatterns)

	// ── Classification logic ──

	// Structure: highly structured if hierarchy density is high
	hierDensity := float64(hierCount) / float64(totalLines)
	numberedDensity := float64(numberedCount) / float64(totalLines)

	if hierDensity > 0.05 || (hierDensity > 0.02 && numberedDensity > 0.1) {
		p.Structure = "highly_structured"
		p.Confidence = clamp(hierDensity * 10, 0.5, 1.0)
	} else if numberedDensity > 0.05 && hierCount > 0 {
		p.Structure = "semi_structured"
		p.Confidence = clamp(numberedDensity*5, 0.5, 0.8)
	} else if dialogDensity(text) > 0.03 || timeWordCount > 5 {
		p.Structure = "unstructured"
		p.Confidence = clamp(float64(timeWordCount)/10.0, 0.5, 0.8)
	}

	// Domain: legal / technical / narrative
	if defCount > 0 && hierCount > 3 {
		p.Domain = "legal"
		p.Confidence = clamp(p.Confidence+0.2, 0.5, 1.0)
	} else if refCount > 2 && (defCount > 0 || hierDensity > 0.03) {
		p.Domain = "legal"
		p.Confidence = clamp(float64(refCount)/10.0, 0.5, 0.9)
	} else if codeCount > 3 {
		p.Domain = "technical"
		p.Confidence = clamp(float64(codeCount)/20.0, 0.5, 0.9)
	} else if dialogDensity(text) > 0.05 && timeWordCount > 3 {
		p.Domain = "narrative"
		p.Confidence = clamp(float64(timeWordCount)/10.0, 0.5, 0.9)
	} else if p.Structure == "highly_structured" {
		p.Domain = "technical"
		p.Confidence = clamp(p.Confidence, 0.5, 0.7)
	}

	return p
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func formatFromExt(ext string) string {
	switch ext {
	case ".md", ".mdx":
		return "markdown"
	case ".pdf":
		return "pdf"
	case ".html", ".htm":
		return "html"
	case ".txt", ".rst":
		return "txt"
	default:
		return "txt"
	}
}

func matchAnyPattern(line string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(line, p) {
			return true
		}
	}
	return false
}

func countMatches(text string, patterns []string) int {
	n := 0
	for _, p := range patterns {
		n += strings.Count(text, p)
	}
	return n
}

// dialogDensity estimates the proportion of dialogue-heavy lines.
func dialogDensity(text string) float64 {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return 0
	}
	dialog := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if len(trimmed) == 0 {
			continue
		}
		quoteCount := 0
		for _, r := range trimmed {
			if r == '"' || r == '「' || r == '『' || r == '「' || r == '『' {
				quoteCount++
			}
		}
		if quoteCount >= 2 {
			dialog++
		}
		// Also count lines starting with " — " or "：" as potential dialogue
		if strings.HasPrefix(trimmed, "——") || strings.HasPrefix(trimmed, "——") {
			dialog++
		}
	}
	return float64(dialog) / float64(len(lines))
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// ── Pattern tables ──────────────────────────────────────────────────

var hierarchyPatterns = []string{
	"第", "章", "节", "条", "款", "项", "编", // Chinese legal
	"§ ", "§\t",
	"Article ", "article ",
	"Section ", "section ",
	"Chapter ", "chapter ",
	"Part ", "part ",
	"Clause ", "clause ",
	"Sub-clause ",
	"Title ", // less discriminative but relevant in context
}

var definitionPatterns = []string{
	"本法所称",
	"shall mean",
	"means ",
	"术语",
	"定义",
	"Definitions",
	"指",
	"所称",
	"用语",
	"Terminology",
}

var referencePatterns = []string{
	"参见第",
	"依照第",
	"根据第",
	"适用第",
	"pursuant to",
	"referred to in",
	"as defined in",
	"subject to section",
	"notwithstanding",
	"herein",
	"hereinafter",
}

var numberedPatterns = []string{
	"1. ", "2. ", "3. ", "4. ", "5. ",
	"1\t", "2\t", "3\t",
	"(a)", "(b)", "(c)", "(d)", "(e)",
	"(1)", "(2)", "(3)", "(4)", "(5)",
	"A.", "B.", "C.",
	"i.", "ii.", "iii.", "iv.",
	"一、", "二、", "三、", "四、", "五、",
	"（一）", "（二）", "（三）",
}

var temporalPatterns = []string{
	"昨天", "今天", "明天",
	"早上", "晚上", "下午",
	"后来", "然后", "接着",
	"三天后", "一周后", "几个月后",
	"before", "after", "then", "later", "suddenly",
	"年", "月", "日",
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

var codePatterns = []string{
	"func ", "func(",
	"class ", "class\t",
	"def ", "def(",
	"import ", "import\t",
	"package ", "package\t",
	"const ", "const\t",
	"var ", "var\t",
	"let ", "let\t",
	"export ", "export\t",
	"public ", "public\t",
	"private ", "private\t",
	"async ", "async\t",
	"return ", "return\t",
	"if ", "if(",
	"for ", "for(",
	"while ", "while(",
}

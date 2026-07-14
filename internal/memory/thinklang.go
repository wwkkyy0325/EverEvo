package memory

import (
	"strings"
	"unicode"
)

// ─── Thinking Language Control ────────────────────────────────────────
//
// Authoritative basis:
//   - Li et al. (2025): Enforcing monolingual decoding reduces accuracy by 5.6pp
//   - Son et al. (KO-REAson, 2025): Language-Mixed CoT improves +18.6pp
//   - Wang et al. (EMNLP 2025): Script-level constraints align with model internals
//   - Mohamed et al. (2025): English tokens in non-English text improve comprehension
//   - AutoCAP (ACL 2024): Automatic language selection outperforms manual choice
//
// Strategy: Query-Driven Anchor Language Selection + Language-Mixed CoT
//
//	Chinese-anchored: Think in Chinese, allow English for code/API/technical terms
//	English-anchored: Think in English, allow Chinese for named entities/quoted text

// ThinkLang is the thinking language mode for a single conversation turn.
type ThinkLang struct {
	Anchor       string  `json:"anchor"`       // "chinese" | "english"
	Confidence   float64 `json:"confidence"`   // 0.0-1.0
	Rule         string  `json:"rule"`         // system prompt injection for thinking control
	AllowMixing  bool    `json:"allowMixing"`  // always true per research consensus
	AllowScripts []string `json:"allowScripts"` // allowed character scripts
}

// ClassifyThinkLang determines the optimal thinking language for a query.
// Per-turn decision: no cross-turn inheritance (OLA, ACL 2026).
func ClassifyThinkLang(query string) ThinkLang {
	if len(strings.TrimSpace(query)) == 0 {
		return ThinkLang{Anchor: "chinese", Confidence: 0.5, AllowMixing: true}
	}

	// ── Character-level analysis ──
	var (
		cjkCount    int
		latinCount  int
		digitCount  int
		symbolCount int
		totalChars  int
	)

	for _, r := range query {
		totalChars++
		switch {
		case unicode.Is(unicode.Han, r):
			cjkCount++
		case unicode.Is(unicode.Latin, r):
			latinCount++
		case unicode.IsDigit(r):
			digitCount++
		default:
			symbolCount++
		}
	}

	if totalChars == 0 {
		return ThinkLang{Anchor: "chinese", Confidence: 0.5, AllowMixing: true}
	}

	cjkRatio := float64(cjkCount) / float64(totalChars)
	latinRatio := float64(latinCount) / float64(totalChars)

	// ── Keyword signals ──
	codeSignals := countMatches(query, []string{
		"func", "class", "def", "import", "const", "var",
		"api", "API", "http", "json", "sql", "npm", "git",
		"代码", "编程", "函数", "接口",
	})
	techSignals := countMatches(query, []string{
		"error", "bug", "fix", "deploy", "build", "test",
		"config", "docker", "server", "client", "module",
	})
	// ── Decision logic ──
	// 3 rules: thinkRuleMixed (default, Chinese query), thinkRuleEnglish, thinkRuleCode

	// Code/tech-heavy → English-dominant with code focus
	if codeSignals > 2 || techSignals > 3 {
		return ThinkLang{
			Anchor: "english", Confidence: clamp(0.6+float64(codeSignals)*0.05, 0.5, 0.9),
			AllowMixing: true, Rule: thinkRuleCode,
		}
	}

	// English-dominant query → English anchor
	if latinRatio > 0.7 {
		return ThinkLang{
			Anchor: "english", Confidence: clamp(latinRatio, 0.7, 0.98),
			AllowMixing: true, Rule: thinkRuleEnglish,
		}
	}

	// Chinese query (default for EverEvo) → Mixed: English logic + Chinese entities
	return ThinkLang{
		Anchor: "chinese", Confidence: clamp(cjkRatio+0.3, 0.6, 0.95),
		AllowMixing: true, Rule: thinkRuleMixed,
	}
}

// ── Thinking rules (injected into system prompt per turn) ──────────
//
// Research basis: Li et al. (EMNLP 2025) — bilingual LLMs exhibit 4 strategic
// code-switching patterns: phrase-level precision, technical terminology,
// format-driven markers, and difficulty-triggered full switches. Forced
// monolingual decoding reduces MATH500 accuracy by 5.6pp (p=0.0017).
//
// Our approach: Language-Mixed CoT (KO-REAson, 2025) with query-driven anchor.
// English = logical scaffold (reasoning, deduction, math, code).
// Chinese = entities, user-facing content, culture-specific concepts.

// thinkRuleMixed is the DEFAULT rule for Chinese user queries.
// English handles logic; Chinese handles entities and user content.
const thinkRuleMixed = `【思考语言规则 — 本回合必须遵守】

你的思考过程采用中英混合模式：

🔤 用英文思考的部分（逻辑层）：
  - 推理步骤、逻辑推导、因果分析
  - 数学计算、公式、数值比较
  - 代码逻辑、算法设计、函数名、变量名
  - 结构化标记："Step 1:", "Analysis:", "Key insight:", "Therefore,"
  - 技术术语无自然中文等价词时保留英文（如 "embedding", "token", "null"）

🀄 用中文保留的部分（实体层）：
  - 人名、地名、公司名、产品名等命名实体
  - 中文原文引用、法律条文、合同条款
  - 文化专有概念（成语、典故、俗语）
  - 用户问题中的中文关键词

✅ 正确示例：
  "Key insight: 张三的合同违约金计算需要分两个阶段。
   Phase 1 (2020-2023): company = A公司, position = 总经理
   Phase 2 (2023-present): company = B公司, position = 副总裁
   Therefore, the penalty calculation uses two different base salaries."

❌ 错误示例（纯中文推理）：
  "关键洞察：张三的合同违约金计算需要分两个阶段..."

❌ 错误示例（纯英文推理）：
  "Key insight: Zhang San's contract penalty calculation needs two phases..."`

// thinkRuleEnglish is for English user queries. Chinese only for entities.
const thinkRuleEnglish = `【Thinking Language Rule — this turn MANDATORY】

Use English for ALL reasoning, logic, and analysis.
Chinese characters ONLY for:
  - Named entities without standard English equivalents (人名, 地名, 公司名)
  - Direct quotations from Chinese source text
  - Culture-specific terms (成语, 典故)
Do NOT translate Chinese entity names into English.`

// thinkRuleCode is for code/technical queries. English-dominant.
const thinkRuleCode = `【Thinking Language Rule — this turn MANDATORY】

English for ALL code, logic, algorithms, and technical analysis.
Chinese ONLY for: user-facing text, Chinese documentation references,
Chinese variable/function names in the target codebase.`

// ─── Helpers ────────────────────────────────────────────────────────

func countMatches(s string, patterns []string) int {
	n := 0
	lower := strings.ToLower(s)
	for _, p := range patterns {
		n += strings.Count(lower, strings.ToLower(p))
	}
	return n
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

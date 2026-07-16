// Package memory — thinking paradigm library (P10).
//
// Paradigms are named thinking methodologies that the AI can SELECT → ADAPT →
// EXECUTE based on the task. Stored as JSON (like skills), not as a graph DB.
// Design follows SELF-DISCOVER (Zhou et al., arXiv:2402.03620) — a library of
// reusable reasoning modules that the LLM self-composes per task.
//
// Traceability follows UpFormat (Gomadam et al., 2025) — each response using a
// paradigm carries a @paradigm JSON marker for feedback collection and stats.
package memory

import (
	"encoding/json"
	"crypto/rand"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
)

// Paradigm is one named thinking methodology in the library.
type Paradigm struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	Icon         string  `json:"icon"`
	Description  string  `json:"description"`
	Methodology  string  `json:"methodology"`
	Applicable   string  `json:"applicable"`
	Example      string  `json:"example"`
	Strength     float64 `json:"strength"`
	UseCount     int     `json:"useCount"`
	SuccessCount int     `json:"successCount"`
	Enabled      bool    `json:"enabled"`
	SourceType   string  `json:"sourceType"`
	LibraryID    string  `json:"libraryId"`
	CreatedAt    int64   `json:"createdAt"`
	UpdatedAt    int64   `json:"updatedAt"`
}

// FeedbackEntry records one structured 3D feedback for a paradigm.
// Stored in-memory only (ring buffer, last N entries per paradigm).
type FeedbackEntry struct {
	ParadigmID       string  `json:"paradigmId"`
	MatchQuality     float64 `json:"matchQuality"`
	ExecutionQuality float64 `json:"executionQuality"`
	OutcomeQuality   float64 `json:"outcomeQuality"`
	Reason           string  `json:"reason"`
	Timestamp        int64   `json:"timestamp"`
}

// ─── Built-in paradigms ─────────────────────────────────────────────────

var builtinParadigms = []Paradigm{
	// ── Analysis ──
	{
		Name: "MECE 分解", Category: "analysis", Icon: "📊", Enabled: true, SourceType: "builtin",
		Description: "相互独立、完全穷尽地分解问题为可管理的子问题",
		Methodology: "## MECE 分解法\n\n1. **定义问题边界**：明确要分析的范围\n2. **选择分解维度**：按功能/时间/结构/因果选择最合适的维度\n3. **分解到 MECE**：确保每个子问题相互独立 + 所有子问题合并完全穷尽\n4. **逐项分析**：对每个子问题独立分析\n5. **汇总洞察**：将各子问题的结论合并，形成整体理解",
		Applicable:  "分析复杂问题时；需要结构化思考时；需要确保不遗漏任何方面时",
		Example:     "问题：「应用启动慢」→ 按阶段分解：启动前(依赖加载、配置读取)→启动中(初始化、数据库连接)→启动后(首屏渲染)。每阶段逐个排查，不遗漏任何环节。",
	},
	{
		Name: "根因分析", Category: "analysis", Icon: "🌳", Enabled: true, SourceType: "builtin",
		Description: "连续追问「为什么」，直到找到问题的根本原因（5 Whys）",
		Methodology: "## 根因分析（5 Whys）\n\n1. **描述问题**：用一句话精确描述发生了什么\n2. **问第一个为什么**：Why did this happen?\n3. **对每个答案继续追问**：Why did THAT happen?\n4. **重复 5 次**（或直到无法继续深入）\n5. **识别根因**：最后一个答案通常是系统性/流程性原因\n6. **制定预防措施**：针对根因，而不是表层症状",
		Applicable:  "调试 bug 时；分析事故根因时；对重复出现的问题找根本解决方案时",
	},
	{
		Name: "系统思维", Category: "analysis", Icon: "🔄", Enabled: true, SourceType: "builtin",
		Description: "把问题看作相互关联的系统，分析反馈回路和涌现行为",
		Methodology: "## 系统思维\n\n1. **画出系统边界**：明确哪些在系统内，哪些是外部环境\n2. **识别要素**：列出系统中的关键实体/变量\n3. **找出连接**：要素之间如何相互影响？画因果回路图\n4. **识别反馈回路**：正反馈（放大）vs 负反馈（稳定）\n5. **寻找杠杆点**：系统中哪些点的小变化会产生大影响？\n6. **预测涌现行为**：从回路互动中预测系统的长期行为趋势",
		Applicable:  "分析复杂动态系统时；问题涉及多方利益相关者时；寻找高影响力干预点时",
	},
	{
		Name: "SWOT 分析", Category: "analysis", Icon: "📋", Enabled: true, SourceType: "builtin",
		Description: "从优势、劣势、机会、威胁四个维度系统评估",
		Methodology: "## SWOT 分析\n\n1. **Strengths 优势**：内部正面因素——我们擅长什么？有什么独特资源？\n2. **Weaknesses 劣势**：内部负面因素——哪里不足？缺什么资源？\n3. **Opportunities 机会**：外部正面因素——市场趋势、技术变革、政策利好\n4. **Threats 威胁**：外部负面因素——竞争对手、替代品、监管风险\n5. **交叉分析**：S×O、W×O、S×T、W×T 四象限策略",
		Applicable:  "制定策略时；评估项目可行性时；竞品分析时",
	},
	{
		Name: "第一性原理", Category: "analysis", Icon: "🔬", Enabled: true, SourceType: "builtin",
		Description: "拆解到最基本元素，从不可再分的真理出发重建",
		Methodology: "## 第一性原理分析\n\n1. **识别假设**：列出关于问题的所有已有假设\n2. **拆解到基础**：逐一追问「这个假设成立的前提是什么？」，直到不能再分\n3. **从零重建**：忽略现有方案，从基础真理出发，重新推导解决方案\n4. **对比验证**：将重建方案与现有方案对比，找出被假设隐藏的创新空间\n\n核心原则：不要接受任何未经检验的前提。Everything is up for debate.",
		Applicable:  "当问题涉及被广泛接受的惯例、传统做法或行业共识时；当现有方案无法突破瓶颈时；当需要根本性创新时",
	},

	// ── Decision ──
	{
		Name: "权衡矩阵", Category: "decision", Icon: "⚖️", Enabled: true, SourceType: "builtin",
		Description: "列出方案，定义权重标准，计算加权得分",
		Methodology: "## 权衡矩阵\n\n1. **列出备选方案**：2-5 个可行选项\n2. **确定评判标准**：成本/时间/质量/风险/可维护性等\n3. **分配权重**：每个标准的相对重要性（总和=100%）\n4. **逐项打分**：每个方案在每个标准下打分（1-5 或 1-10）\n5. **计算加权分数**：Σ(权重 × 分数)\n6. **敏感性分析**：改动权重，看排名是否稳定\n7. **决策 + 记录理由**",
		Applicable:  "多方案选择时；需要向他人证明决策合理性时；团队决策有分歧时",
	},
	{
		Name: "事前验尸", Category: "decision", Icon: "🔮", Enabled: true, SourceType: "builtin",
		Description: "假设方案已经失败，倒推找出所有可能的失败原因",
		Methodology: "## 事前验尸（Pre-mortem）\n\n1. **假设失败**：现在是未来某一天，方案已经失败了\n2. **脑暴失败原因**：每个人独立写下「为什么失败了？」\n3. **分类汇总**：按技术/市场/团队/外部等维度归类\n4. **评估概率+影响**：每个原因的发生概率和影响程度\n5. **制定预防措施**：针对高概率+高影响的原因制定应对方案\n6. **更新计划**：把应对方案纳入执行计划",
		Applicable:  "重大决策前；项目启动前；看似万无一失的方案验证时",
	},
	{
		Name: "逆向思维", Category: "decision", Icon: "🪞", Enabled: true, SourceType: "builtin",
		Description: "从期望的终点倒推路径，或从反面思考问题",
		Methodology: "## 逆向思维\n\n1. **定义目标状态**：你想要的最终结果是什么？\n2. **倒推**：要达到目标，前一步必须是什么？\n3. **继续倒推**：直到到达当前状态\n4. **审视路径**：每一步是否可行？\n5. **替代问法**：反过来想——「如何让问题变得更糟？」→ 答案的反面就是解法",
		Applicable:  "制定行动计划时；正向思考陷入困境时；需要创新解法时",
	},

	// ── Creative ──
	{
		Name: "类比推理", Category: "creative", Icon: "💡", Enabled: true, SourceType: "builtin",
		Description: "找到相似问题的解决方案，映射到当前问题",
		Methodology: "## 类比推理\n\n1. **抽象当前问题**：提取核心特征（如「资源分配」「模式匹配」「调度优化」）\n2. **寻找类比域**：在自然界/其他行业/历史中找到类似结构的问题\n3. **提取解决模式**：类比域中是如何解决的？核心机制是什么？\n4. **映射回来**：将解决模式映射到当前问题的具体语境\n5. **适应性调整**：调整细节以适配当前约束",
		Applicable:  "传统方法无效时；跨领域创新时；需要跳出固有思维框架时",
	},
	{
		Name: "SCAMPER 头脑风暴", Category: "creative", Icon: "🧠", Enabled: true, SourceType: "builtin",
		Description: "从 7 个维度系统性地激发创新：替代/合并/调整/修改/他用/消除/重排",
		Methodology: "## SCAMPER 头脑风暴\n\n1. **Substitute 替代**：有什么可以替换？材料/流程/人员？\n2. **Combine 合并**：能和什么结合？合并功能/步骤/资源？\n3. **Adapt 调整**：能借鉴其他领域的什么做法？\n4. **Modify 修改**：放大/缩小/改变形状/改变属性？\n5. **Put to another use 他用**：还能用来做什么？换个场景？\n6. **Eliminate 消除**：去掉什么？简化什么？\n7. **Rearrange 重排**：颠倒顺序？换位？重新组织？\n\n逐项问自己，记录所有想法，不评判。最后选出 3 个最有价值的。",
		Applicable:  "产品设计时；功能迭代时；流程优化时；需要大量创意时",
	},

	// ── Debug ──
	{
		Name: "二分排除法", Category: "debug", Icon: "🔍", Enabled: true, SourceType: "builtin",
		Description: "每次排除一半的可能性，快速定位问题",
		Methodology: "## 二分排除法\n\n1. **确定搜索空间**：问题的可能范围——代码/配置/数据/环境\n2. **对半分**：将搜索空间分成两半\n3. **验证一半**：排除一半（如注释掉一半代码，测试是否还出问题）\n4. **重复**：在剩余的一半中再对半分\n5. **定位根因**：在 log2(N) 步内找到具体原因。log2(1000) ≈ 10 步。",
		Applicable:  "代码调试时；定位异常数据时；排查环境问题时",
	},
	{
		Name: "假设检验驱动调试", Category: "debug", Icon: "🧪", Enabled: true, SourceType: "builtin",
		Description: "提出假设→设计实验→验证→修正假设→重复，科学方法调试",
		Methodology: "## 假设检验驱动调试\n\n1. **观察症状**：精确记录发生了什么（不要推测）\n2. **生成假设**：列出所有可能的原因（至少 3 个）\n3. **设计验证实验**：每个假设如何验证？用什么命令/日志/测试？\n4. **按概率排序**：优先验证最可能的原因\n5. **执行验证**：逐一验证，记录结果\n6. **接受或拒绝**：确认的→修复；排除的→移除\n7. **重复**：如果所有假设都被排除，回到步骤 1 重新观察",
		Applicable:  "复杂 bug 调试时；非确定性问题的排查时",
	},

	// ── Planning ──
	{
		Name: "分治法", Category: "planning", Icon: "🗂️", Enabled: true, SourceType: "builtin",
		Description: "把大任务递归分解为小任务直到可独立执行",
		Methodology: "## 分治法\n\n1. **定义主任务**：用一句话描述最终目标\n2. **分解**：如果任务太大不能一步完成，分解为 2-5 个子任务\n3. **递归**：对每个子任务重复步骤 2，直到每个子任务都足够小\n4. **排序**：按依赖关系排序子任务\n5. **执行**：从无依赖的子任务开始逐个完成\n6. **合并**：将子任务结果组装成最终成果",
		Applicable:  "大项目规划时；任务拆解时；工作量估算时",
	},
	{
		Name: "SMART 目标", Category: "planning", Icon: "🎯", Enabled: true, SourceType: "builtin",
		Description: "确保目标是具体的、可衡量的、可达到的、相关的、有时间限制的",
		Methodology: "## SMART 目标法\n\n1. **Specific 具体**：目标要明确，不能模糊\n2. **Measurable 可衡量**：用数字定义成功\n3. **Achievable 可实现**：有挑战但可行\n4. **Relevant 相关**：与更大目标一致\n5. **Time-bound 有时限**：明确截止日期\n\n对每个目标逐项检查这五个维度。",
		Applicable:  "制定计划时；向他人描述目标时；评估目标合理性时",
	},

	// ── Metacognitive ──
	{
		Name: "ReAct 标准", Category: "analysis", Icon: "🔁", Enabled: true, SourceType: "builtin",
		Description: "推理-行动循环：分析→行动→观察→重复→最终回答",
		Methodology: "## ReAct（Reasoning-Act）框架\n\n1. **Thought 分析**：理解用户意图，判断需要什么信息、调用哪些工具\n2. **Action 行动**：选择合适的工具，用精确的参数调用\n3. **Observation 观察**：仔细阅读工具返回结果，发现新的信息\n4. **重复**：如果信息不足，回到步骤 1，用新的理解再次行动\n5. **Final Answer 最终回答**：掌握足够信息后，用简洁的语言直接回复\n\n核心原则：先思考再行动，失败时分析原因尝试替代方案。这是所有其他范式的元框架。",
		Applicable:  "需要调用工具的查询/操作；多步骤信息收集任务",
	},
	{
		Name: "结构化诊断分析", Category: "analysis", Icon: "🩺", Enabled: true, SourceType: "builtin",
		Description: "系统审计/Bug 根因分析：信号收集→根因归类→优先级排序",
		Methodology: "## 结构化诊断分析\n\n三层诊断框架，建立在 ReAct 基础上：\n\n### 第一层：信号收集\n- 扫描所有可用信息源：记忆、历史问答、Wiki、任务板、日志\n- 提取所有异常信号、不一致、已知缺陷\n- 不做判断，只收集原始证据\n\n### 第二层：根因归类\n- 把症状归到同一根因下——追问「这些 bug 是不是同一个根因的不同表现？」\n- 归类维度：架构缺陷、数据错误、流程缺失、配置问题、边界条件\n- 每组输出一个根因假说\n\n### 第三层：优先级排序\n- 按 影响面 × 紧急度 打分\n- 致命 > 严重 > 中等 > 体验\n- 输出 Top 3 优先修复项\n\n核心原则：不孤立看问题，追问根因。",
		Applicable:  "系统审计；Bug 根因分析；代码审查总结；项目健康检查",
	},
	{
		Name: "对比决策", Category: "decision", Icon: "⚖️", Enabled: true, SourceType: "builtin",
		Description: "A vs B 方案选择：列出维度→逐维对比→加权评分",
		Methodology: "## 对比决策法\n\n1. **明确决策目标**：一句话描述你要决定什么\n2. **列出备选方案**：2-4 个可行选项\n3. **确定评估维度**：性能、复杂度、可维护性、成本、风险、生态等\n4. **逐维对比**：每个维度，A 和 B 分别表现如何？用具体数据/论据\n5. **加权评分**：为每个维度分配权重，计算加权总分\n6. **敏感性检查**：改动权重，排名是否稳定？\n7. **输出推荐**：推荐得分最高的方案，说明在什么条件下备选更优\n\n如果信息不足，主动列出需要补充的信息，不要盲目打分。",
		Applicable:  "技术选型时；方案评审时；需要向他人证明决策合理性时",
	},
	{
		Name: "逐步执行", Category: "planning", Icon: "🪜", Enabled: true, SourceType: "builtin",
		Description: "多步骤代码修改/部署：计划拆解→逐步执行→每步验证",
		Methodology: "## 逐步执行法\n\n1. **理解目标**：明确最终要达到的状态\n2. **拆解步骤**：将任务分解为 3-7 个有序步骤，每步有明确的输入→输出\n3. **预估风险**：标注每步的风险等级和可能的失败模式\n4. **逐步执行**：执行第 N 步 → 验证编译/测试 → 成功后进入 N+1\n5. **失败回滚**：如果某步失败，分析原因→修正→重试，或回滚\n6. **最终验证**：所有步骤完成后，端到端验证整体功能\n\n核心原则：每步独立可验证，永远有退路。",
		Applicable:  "多文件代码修改；部署流程；数据库迁移；配置变更",
	},
	{
		Name: "反思优化", Category: "analysis", Icon: "🪞", Enabled: true, SourceType: "builtin",
		Description: "对自己的输出进行自我审查：查漏补缺→逻辑校验→可读性优化",
		Methodology: "## 反思优化法\n\n在给出最终回答前，执行以下自检：\n\n1. **完整性检查**：是否回答了用户的所有问题？有没有遗漏的子问题？\n2. **一致性检查**：回答的各个部分之间有无矛盾？结论和论据是否一致？\n3. **准确性检查**：引用的代码路径、文件名、API 签名是否真实存在？\n4. **清晰性检查**：语言是否简洁？结构是否清晰？不需要的信息是否已删减？\n5. **边界说明**：明确说明方案的适用范围和局限性。不要过度承诺。\n\n如果发现任何问题，修正后再输出。这是所有范式执行完后的最后一道门禁。",
		Applicable:  "所有需要高质量输出的场景；复杂分析后；代码修改方案输出前",
	},
}

// ─── Manager ───────────────────────────────────────────────────────────

// Embedder is a function that embeds text into a vector. Wired by app layer.
type Embedder func(text string) ([]float32, error)

type ParadigmManager struct {
	Paradigms       []Paradigm                `json:"paradigms"`
	feedbackHistory map[string][]FeedbackEntry // paradigm ID → ring buffer (last 20)
	embedder        Embedder                  // optional: enables semantic matching in Recommend
	embeddings      map[string][]float32       // paradigm ID → pre-computed embedding vector
}

func paradigmPath() string {
	dir, err := storage.AppDataDir()
	if err != nil {
		dir = "data"
	}
	return filepath.Join(dir, "paradigms.json")
}

func NewParadigmManager() *ParadigmManager {
	m := &ParadigmManager{
		feedbackHistory: make(map[string][]FeedbackEntry),
	}
	loaded := loadParadigms()
	if loaded != nil {
		m.Paradigms = loaded
			// Repair duplicate IDs (caused by pre-fix paradigmID that used high
			// hex digits of timestamp — all builtins seeded in the same boot
			// share the same ID). Detect and regenerate.
			seenIDs := map[string]bool{}
			hasDup := false
			for _, p := range m.Paradigms {
				if seenIDs[p.ID] {
					hasDup = true
					break
				}
				seenIDs[p.ID] = true
			}
			if hasDup {
				log.Printf("[paradigm] detected duplicate IDs — regenerating all IDs")
				for i := range m.Paradigms {
					m.Paradigms[i].ID = paradigmID()
				}
				_ = m.Save()
			}
		existing := map[string]bool{}
		for _, p := range m.Paradigms {
			existing[p.Name] = true
		}
		added := 0
		now := time.Now().UnixMilli()
		for _, bp := range builtinParadigms {
			if !existing[bp.Name] {
				bp.ID = paradigmID()
				bp.CreatedAt = now
				bp.UpdatedAt = now
				m.Paradigms = append(m.Paradigms, bp)
				added++
			}
		}
		if added > 0 {
			log.Printf("[paradigm] loaded %d + added %d builtins", len(loaded), added)
			_ = m.Save()
		} else {
			log.Printf("[paradigm] loaded %d paradigms", len(m.Paradigms))
		}
		return m
	}
	now := time.Now().UnixMilli()
	for i := range builtinParadigms {
		builtinParadigms[i].ID = paradigmID()
		builtinParadigms[i].CreatedAt = now
		builtinParadigms[i].UpdatedAt = now
	}
	paradigms := make([]Paradigm, len(builtinParadigms))
	copy(paradigms, builtinParadigms)
	m.Paradigms = paradigms
	log.Printf("[paradigm] seeded %d built-in paradigms", len(m.Paradigms))
	return m
}

func loadParadigms() []Paradigm {
	path := paradigmPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p []Paradigm
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("[paradigm] parse %s failed: %v", path, err)
		return nil
	}
	if len(p) == 0 {
		return nil
	}
	return p
}

func (m *ParadigmManager) Save() error {
	path := paradigmPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("paradigm: create dir: %w", err)
	}
	data, err := json.MarshalIndent(m.Paradigms, "", "  ")
	if err != nil {
		return fmt.Errorf("paradigm: marshal: %w", err)
	}
	if err := atomic.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("paradigm: write: %w", err)
	}
	return nil
}

func (m *ParadigmManager) List() []Paradigm { return m.Paradigms }

func (m *ParadigmManager) ListEnabled(libraryID string) []Paradigm {
	var out []Paradigm
	for _, p := range m.Paradigms {
		if !p.Enabled {
			continue
		}
		if libraryID == "" || p.LibraryID == "" || p.LibraryID == libraryID {
			out = append(out, p)
		}
	}
	return out
}

func (m *ParadigmManager) Get(id string) (*Paradigm, error) {
	for i := range m.Paradigms {
		if m.Paradigms[i].ID == id {
			return &m.Paradigms[i], nil
		}
	}
	return nil, fmt.Errorf("paradigm %q not found", id)
}

func (m *ParadigmManager) FindByName(name string) (*Paradigm, error) {
	for i := range m.Paradigms {
		if m.Paradigms[i].Name == name {
			return &m.Paradigms[i], nil
		}
	}
	return nil, fmt.Errorf("paradigm %q not found", name)
}

func (m *ParadigmManager) Add(p Paradigm) (*Paradigm, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("paradigm name required")
	}
	for _, e := range m.Paradigms {
		if e.Name == p.Name {
			return nil, fmt.Errorf("paradigm %q already exists", p.Name)
		}
	}
	now := time.Now().UnixMilli()
	p.ID = paradigmID()
	if p.SourceType == "" {
		p.SourceType = "manual"
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	m.Paradigms = append(m.Paradigms, p)
	if err := m.Save(); err != nil {
		return nil, err
	}
	return &m.Paradigms[len(m.Paradigms)-1], nil
}

func (m *ParadigmManager) Update(id string, p Paradigm) error {
	for i := range m.Paradigms {
		if m.Paradigms[i].ID == id {
			p.ID = id
			p.CreatedAt = m.Paradigms[i].CreatedAt
			p.UpdatedAt = time.Now().UnixMilli()
			m.Paradigms[i] = p
			return m.Save()
		}
	}
	return fmt.Errorf("paradigm %q not found", id)
}

func (m *ParadigmManager) Delete(id string) error {
	for i := range m.Paradigms {
		if m.Paradigms[i].ID == id {
			if m.Paradigms[i].SourceType != "" && m.Paradigms[i].SourceType != "manual" {
				m.Paradigms[i].Enabled = false
				return m.Save()
			}
			m.Paradigms = append(m.Paradigms[:i], m.Paradigms[i+1:]...)
			return m.Save()
		}
	}
	return fmt.Errorf("paradigm %q not found", id)
}

func (m *ParadigmManager) SetEnabled(id string, enabled bool) error {
	for i := range m.Paradigms {
		if m.Paradigms[i].ID == id {
			m.Paradigms[i].Enabled = enabled
			m.Paradigms[i].UpdatedAt = time.Now().UnixMilli()
			return m.Save()
		}
	}
	return fmt.Errorf("paradigm %q not found", id)
}

func (m *ParadigmManager) Feedback(id string, matchQ, execQ, outcomeQ float64, reason string) (float64, error) {
	for i := range m.Paradigms {
		if m.Paradigms[i].ID == id {
			m.Paradigms[i].UseCount++
			now := time.Now().UnixMilli()
			m.Paradigms[i].UpdatedAt = now
			// Composite strength: weighted average of 3 dimensions.
			// matchQ=0.5 means "wrong paradigm for this task" — don't penalize the paradigm.
			// execQ=0.5 means "paradigm methodology needs improvement".
			// outcomeQ=0.5 means "final output wasn't useful".
			// We weight: match=0.15 (mostly caller's fault), exec=0.45, outcome=0.40.
			composite := matchQ*0.15 + execQ*0.45 + outcomeQ*0.40
			if composite >= 0.6 {
				m.Paradigms[i].SuccessCount++
			}
			// Smooth update (EMA-style) rather than jump.
			old := m.Paradigms[i].Strength
			m.Paradigms[i].Strength = clamp(old*0.7+composite*0.3, 0, 1)

			// Store feedback in ring buffer.
			m.feedbackHistory[id] = append(m.feedbackHistory[id], FeedbackEntry{
				ParadigmID:       id,
				MatchQuality:     matchQ,
				ExecutionQuality: execQ,
				OutcomeQuality:   outcomeQ,
				Reason:           reason,
				Timestamp:        now,
			})
			if len(m.feedbackHistory[id]) > 20 {
				m.feedbackHistory[id] = m.feedbackHistory[id][len(m.feedbackHistory[id])-20:]
			}
			return composite, m.Save()
		}
	}
	return 0, fmt.Errorf("paradigm %q not found", id)
}

// FeedbackHistory returns the last N feedback entries for a paradigm.
func (m *ParadigmManager) FeedbackHistory(id string, limit int) []FeedbackEntry {
	h := m.feedbackHistory[id]
	if len(h) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(h) {
		limit = len(h)
	}
	return h[len(h)-limit:]
}

// ListNames returns the names of all enabled paradigms (for LLM context).
func (m *ParadigmManager) ListNames() []string {
	var names []string
	for _, p := range m.Paradigms {
		if p.Enabled {
			names = append(names, p.Name)
		}
	}
	return names
}

// SetEmbedder wires an embedding function for semantic paradigm matching.
// Call BuildEmbeddings after setting to pre-compute paradigm vectors.
func (m *ParadigmManager) SetEmbedder(e Embedder) {
	m.embedder = e
}

// BuildEmbeddings pre-computes embedding vectors for all enabled paradigms.
// The profile text used for embedding is: name + " " + description + " " + applicable + " " + category.
// Call this after SetEmbedder to enable semantic matching in Recommend.
func (m *ParadigmManager) BuildEmbeddings() error {
	if m.embedder == nil {
		return fmt.Errorf("paradigm: embedder not set")
	}
	m.embeddings = make(map[string][]float32, len(m.Paradigms))
	for _, p := range m.Paradigms {
		if !p.Enabled {
			continue
		}
		profile := p.Name + " " + p.Description + " " + p.Applicable + " " + p.Category
		vec, err := m.embedder(profile)
		if err != nil {
			log.Printf("[paradigm] embed %s failed: %v", p.Name, err)
			continue
		}
		m.embeddings[p.ID] = vec
	}
	log.Printf("[paradigm] built embeddings for %d/%d paradigms", len(m.embeddings), len(m.Paradigms))
	return nil
}

// Recommend returns the top N paradigms ranked by hybrid score:
//
//	score = 0.5 × cosine_similarity(task_embedding, paradigm_embedding)
//	      + 0.3 × keyword_boost(task, paradigm_metadata)
//	      + 0.2 × historical_strength
//
// If no embedder is configured, falls back to keyword + strength scoring.
func (m *ParadigmManager) Recommend(taskDescription string, topN int) []Paradigm {
	type scored struct {
		p     Paradigm
		score float64
	}
	var ranked []scored

	// Try embedding-based scoring first
	var taskVec []float32
	if m.embedder != nil {
		if vec, err := m.embedder(taskDescription); err == nil {
			taskVec = vec
		}
	}

	for _, p := range m.Paradigms {
		if !p.Enabled {
			continue
		}
		score := p.Strength * 0.3 // base: historical success weight

		// Semantic similarity (0.5 weight) if embeddings available
		if taskVec != nil && m.embeddings != nil {
			if pVec, ok := m.embeddings[p.ID]; ok {
				sim := cosineSimilarity(taskVec, pVec) // 0.0-1.0
				score += sim * 0.5
			}
		}

		// Keyword boost (0.2 weight) — always available
		kwScore := keywordMatchScore(taskDescription, p)
		score += kwScore * 0.2

		ranked = append(ranked, scored{p: p, score: score})
	}

	// Sort descending by score
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[i].score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	if len(ranked) > topN {
		ranked = ranked[:topN]
	}
	out := make([]Paradigm, len(ranked))
	for i, r := range ranked {
		out[i] = r.p
	}
	return out
}

// ─── Scoring helpers ─────────────────────────────────────────────────

// cosineSimilarity computes the cosine similarity between two float32 vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// keywordMatchScore returns 0.0-1.0 based on keyword overlap between the task
// description and the paradigm's metadata fields.
func keywordMatchScore(task string, p Paradigm) float64 {
	taskLower := strings.ToLower(task)
	var hits int

	// Paradigm name as a whole token
	if strings.Contains(taskLower, strings.ToLower(p.Name)) {
		hits += 3
	}
	// Category match (debug queries → debug paradigms, etc.)
	if strings.Contains(taskLower, p.Category) {
		hits += 2
	}
	// Keywords from description, applicable, methodology
	keywords := extractKeywords(p)
	for _, kw := range keywords {
		if strings.Contains(taskLower, strings.ToLower(kw)) {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return math.Min(float64(hits)*0.12, 1.0)
}

// extractKeywords pulls domain-significant words from paradigm metadata.
func extractKeywords(p Paradigm) []string {
	text := p.Description + " " + p.Applicable + " " + p.Example
	words := strings.Fields(text)
	var out []string
	for _, w := range words {
		w = strings.Trim(w, "，,。.、；;：:（）()")
		if len([]rune(w)) >= 2 {
			out = append(out, w)
		}
	}
	// Deduplicate
	seen := make(map[string]bool, len(out))
	var result []string
	for _, w := range out {
		lower := strings.ToLower(w)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, w)
		}
	}
	return result
}

func (m *ParadigmManager) EnsureLibraryIDs(defaultID string, validIDs []string) error {
	valid := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}
	changed := false
	for i := range m.Paradigms {
		if m.Paradigms[i].LibraryID == "" || !valid[m.Paradigms[i].LibraryID] {
			m.Paradigms[i].LibraryID = defaultID
			changed = true
		}
	}
	if changed {
		return m.Save()
	}
	return nil
}

func paradigmID() string {
	// 6 random bytes = 12 hex chars = 2^48 space.
	// Timestamp-based IDs collide when seeding 19 builtins in a tight loop
	// because the high hex digits of UnixNano barely change.
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("pd_%x", b)
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

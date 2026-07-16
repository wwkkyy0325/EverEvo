// Conversion between Workflow model and Vue Flow model.

const TYPE_META: Record<string, { icon: string; label: string; desc: string }> = {
  input:     { icon: '⇩', label: '输入', desc: '定义工作流入参' },
  llm:       { icon: '◎', label: 'LLM 调用', desc: '调用大语言模型' },
  tool:      { icon: '⊞', label: '工具调用', desc: '调用 EverEvo 工具' },
  condition: { icon: '⇆', label: '条件分支', desc: '根据表达式分支' },
  code:      { icon: '{}', label: '代码变换', desc: '模板文本转换' },
  loop:      { icon: '↻', label: '循环', desc: '遍历数组执行子图' },
  agent:     { icon: '🤖', label: '智能体', desc: '调用本地 Agent（人格）执行子任务' },
  output:    { icon: '⇧', label: '输出', desc: '收集并输出结果' },
  http:      { icon: '🌐', label: 'HTTP 请求', desc: '调用外部 API 接口' },
  delay:     { icon: '⏱️', label: '延时等待', desc: '暂停指定时长后继续' },
  notify:    { icon: '🔔', label: '通知', desc: '发送桌面通知或消息' },
  merge:     { icon: '⫴', label: '合并', desc: '等待多个输入汇合后继续' },
  custom:    { icon: '⚙', label: '自定义', desc: '自定义脚本/逻辑节点' },
}

export { TYPE_META }

export function flowToWorkflowNode(fn: any) {
  return {
    id: fn.id,
    type: fn.data?.type || fn.type || '',
    title: fn.data?.label || '',
    description: fn.data?.desc || '',
    config: fn.data?.config || {},
    position: fn.position ? { x: Math.round(fn.position.x), y: Math.round(fn.position.y) } : undefined,
  }
}

export function workflowToFlowNode(wn: any, nodeRuns?: Record<string, any>) {
  const meta = TYPE_META[wn.type] || { icon: '?', label: wn.type, desc: '' }
  return {
    id: wn.id,
    type: 'workflow',
    // Missing positions are resolved by the caller's auto-layout pass (see
    // WorkflowEditor.restoreFlowFromWorkflow), not by random dice here.
    position: wn.position ? { x: wn.position.x, y: wn.position.y } : undefined,
    data: {
      type: wn.type,
      icon: meta.icon || '?',
      label: wn.title || meta.label || wn.type,
      typeLabel: meta.label || wn.type,
      desc: wn.description || '',
      config: wn.config || {},
      status: nodeRuns ? ((nodeRuns[wn.id] || {}).status || 'pending') : 'pending',
      trueLabel: (wn.config || {}).trueLabel || 'True',
      falseLabel: (wn.config || {}).falseLabel || 'False',
    },
    draggable: true,
    selectable: true,
  }
}

export function workflowEdgeToFlowEdge(we: any) {
  // Edges authored by the LLM (or imported) may lack an id. Vue Flow keys edges
  // by id, so multiple empty ids collide and connections vanish. Fall back to a
  // deterministic id matching the backend EdgeID format (source→handle→target).
  const id = we.id || (we.source + '→' + (we.sourceHandle || 'output') + '→' + we.target)
  return {
    id,
    source: we.source,
    target: we.target,
    sourceHandle: we.sourceHandle || undefined,
    type: 'smoothstep',
    animated: false,
    style: { stroke: 'var(--border-soft)', strokeWidth: 2 },
    data: { label: we.sourceHandle === 'true' ? 'T' : we.sourceHandle === 'false' ? 'F' : '' },
  }
}

export function flowEdgeToWorkflowEdge(fe: any) {
  return {
    id: fe.id,
    source: fe.source,
    target: fe.target,
    sourceHandle: fe.sourceHandle || 'output',
  }
}

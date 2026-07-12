/**
 * Workflow API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface WorkflowNodePos {
  x: number
  y: number
}

export interface WorkflowNode {
  id: string
  type?: string
  title?: string
  description?: string
  config?: any
  position?: WorkflowNodePos
}

export interface Workflow {
  id: string
  name: string
  nodes: WorkflowNode[]
  edges: any[]
  createdAt: number
  updatedAt: number
}

export const workflowApi = {
  list() { return call<Workflow[]>(() => App().ListWorkflows()) },
  get(id: string) { return call<Workflow>(() => App().GetWorkflow(id)) },
  create(data: Partial<Workflow>) { return call<Workflow>(() => App().CreateWorkflow(data)) },
  update(id: string, data: Partial<Workflow>) { return call(() => App().UpdateWorkflow(id, data)) },
  remove(id: string) { return call(() => App().DeleteWorkflow(id)) },
  duplicate(id: string) { return call<Workflow>(() => App().DuplicateWorkflow(id)) },
  export(id: string) { return call<string>(() => App().ExportWorkflow(id)) },
  import(data: any) { return call(() => App().ImportWorkflow(data)) },
  execute(id: string, inputs: Record<string, any>) { return call<string>(() => App().ExecuteWorkflow(id, inputs)) },
  cancel(id: string) { return call(() => App().CancelWorkflowExecution(id)) },
  validate(id: string) { return call<any>(() => App().ValidateWorkflow(id)) },
  getExecutionStatus(id: string) { return call<any>(() => App().GetWorkflowExecutionStatus(id)) },
}

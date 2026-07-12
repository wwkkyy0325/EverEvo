/**
 * Collaboration API — multi-agent collaboration primitives.
 *
 * Sessions, blackboard, and async dispatch are exposed to both the UI and the
 * LLM (via collab_* tools). The backend Kernel owns the in-process event bus.
 */
import { call } from './client'

const App = () => window.go.app.App

export interface CollabMember {
  agentId: string
  role: string
}

export interface CollabSession {
  id: string
  goal: string
  orchestratorId: string
  status: string
  members: CollabMember[]
  blackboardId: string
  createdAt: string
}

export interface BlackboardEntry {
  key: string
  value: string
  author: string
  kind: string
  updatedAt: string
}

export interface AgentRunResult {
  runId: string
  agentId?: string
  status?: string
  result?: string
  error?: string
}

export interface PlanStep {
  index: number
  title: string
  status: string   // pending | in_progress | done | skipped
  note?: string
  agentId?: string
  updatedAt: string
}

export interface Plan {
  id: string
  sessionId?: string
  goal: string
  steps: PlanStep[]
  status: string   // active | completed | abandoned
  author: string
  createdAt: string
  updatedAt: string
}

export interface CollabEvent {
  topic: string
  data: {
    topic: string
    data: {
      topic?: string
      source?: string
      type?: string
      payload?: any
      at?: string
    }
  }
}

/** One recorded AI-work event in the unified activity timeline. */
export interface ActivityRow {
  id: string
  ts: number
  kind: string        // agent_start | agent_done | tool_call | workflow_start | workflow_node | workflow_done | session | plan | blackboard
  topic: string
  source: string      // agentId / execId
  sourceName: string  // resolved display name (agent name / workflow name)
  sessionId: string
  summary: string
  payload: string     // original event JSON, for detail/replay
}

export const collabApi = {
  /** Create a new multi-agent collaboration session with a shared blackboard. */
  create(goal: string, orchestratorId: string, members: CollabMember[]): Promise<{ sessionId: string; blackboardId: string; message: string }> {
    return call(() => App().CollabCreate(goal, orchestratorId, members))
  },

  /** List all active collaboration sessions. */
  listSessions(): Promise<CollabSession[]> {
    return call(() => App().CollabListSessions())
  },

  /** Finish a session and drop its blackboard. */
  complete(sessionId: string): Promise<void> {
    return call(() => App().CollabComplete(sessionId))
  },

  /** Send a task to an agent synchronously (blocks until done). */
  send(targetAgentId: string, task: string): Promise<string> {
    return call(() => App().CollabSend(targetAgentId, task))
  },

  /** Dispatch a task asynchronously, returning a runId immediately. */
  dispatchAsync(sessionId: string, targetAgentId: string, task: string): Promise<{ runId: string; message: string }> {
    return call(() => App().CollabDispatchAsync(sessionId, targetAgentId, task))
  },

  /** Wait for one or more async runs to finish and return their results. */
  wait(runIds: string[]): Promise<AgentRunResult[]> {
    return call(() => App().CollabWait(runIds))
  },

  /** Write a key to a session's blackboard. */
  bbSet(sessionId: string, key: string, value: string, author: string, kind: string): Promise<boolean> {
    return call(() => App().CollabBbSet(sessionId, key, value, author, kind))
  },

  /** Read one key from a blackboard. */
  bbGet(sessionId: string, key: string): Promise<{ found: boolean; entry?: BlackboardEntry }> {
    return call(() => App().CollabBbGet(sessionId, key))
  },

  /** List all blackboard entries. */
  bbList(sessionId: string): Promise<BlackboardEntry[]> {
    return call(() => App().CollabBbList(sessionId))
  },

  /** Create a task plan (goal → ordered steps). */
  planCreate(goal: string, steps: string[], author: string): Promise<{ planId: string; stepCount: number; message: string }> {
    return call(() => App().PlanCreate(goal, steps, author))
  },

  /** Update a plan step's status. */
  planStepUpdate(planId: string, stepIndex: number, status: string, note: string, agentId: string): Promise<void> {
    return call(() => App().PlanStepUpdate(planId, stepIndex, status, note, agentId))
  },

  /** List all plans. */
  planList(): Promise<Plan[]> {
    return call(() => App().PlanList())
  },

  /** Unified AI-work timeline (history/replay), newest-first. */
  listActivity(kind = '', sessionId = '', source = '', since = 0, limit = 0): Promise<ActivityRow[]> {
    return call(() => App().ListActivity(kind, sessionId, source, since, limit))
  },
}

/**
 * Zone management API — runtime zone (运行区) operations.
 *
 * A "zone" is an isolated instance environment under
 * %APPDATA%/EverEvo/zones/<name>/. Each zone has its own config,
 * agents, memory, wiki, knowledge, and workflows. Models and cache
 * are shared across all zones.
 */
import { call } from './client'

const App = () => window.go.app.App

export interface Zone {
  name: string
  dir: string
  type: 'production' | 'experiment' | 'backup'
  parent: string
  pid: number
  mcpPort: number
  a2aPort: number
  createdAt: string
}

export interface EvolveCapability {
  sourceAvailable: boolean
  sourceDir: string
  goAvailable: boolean
  nodeAvailable: boolean
  wailsAvailable: boolean
}

export interface BuildResult {
  success: boolean
  outputExe: string
  duration: string
  steps: string[]
}

export const zoneApi = {
  /** Returns the current zone's info. */
  getCurrent(): Promise<Zone> {
    return call(() => App().GetCurrentZone())
  },

  /** Lists all zones (production, experiments, backups). */
  list(): Promise<Zone[]> {
    return call(() => App().ListZones())
  },

  /** Copies production into a new experiment zone. */
  createExperiment(name: string): Promise<Zone & { message: string }> {
    return call(() => App().CreateExperiment(name))
  },

  /** Launches a zone's EverEvo instance (opens a new window). */
  launch(name: string): Promise<{ message: string }> {
    return call(() => App().LaunchZone(name))
  },

  /** Stops a running zone process. */
  stop(name: string): Promise<{ message: string }> {
    return call(() => App().StopZone(name))
  },

  /** Merges an experiment zone into production (backup first). */
  merge(name: string): Promise<{ message: string }> {
    return call(() => App().MergeZone(name))
  },

  /** Discards (deletes) an experiment zone. */
  discard(name: string): Promise<{ message: string }> {
    return call(() => App().DiscardZone(name))
  },

  // ─── Backups ───

  /** Lists all backup zones. */
  listBackups(): Promise<Zone[]> {
    return call(() => App().ListBackups())
  },

  /** Creates a manual backup of production. */
  createBackup(): Promise<{ name: string; message: string }> {
    return call(() => App().CreateBackup())
  },

  /** Restores production from a backup. */
  restoreBackup(name: string): Promise<{ message: string }> {
    return call(() => App().RestoreBackup(name))
  },

  /** Deletes a backup zone. */
  deleteBackup(name: string): Promise<{ message: string }> {
    return call(() => App().DeleteBackup(name))
  },

  // ─── Self-Evolution ───

  /** Checks whether source-level self-evolution is available. */
  getEvolveCapability(): Promise<EvolveCapability> {
    return call(() => App().GetEvolveCapability())
  },

  /** Builds the entire project from source (frontend + Go + Wails). */
  buildSelf(): Promise<BuildResult> {
    return call(() => App().BuildSelf())
  },

  /** Swaps the running EXE with the newly-built one and restarts. */
  swapAndRestart(): Promise<{ message: string }> {
    return call(() => App().SwapAndRestart())
  },
}

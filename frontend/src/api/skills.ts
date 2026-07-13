/**
 * Skill / Plugin / Guide API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface Skill {
  name: string
  title: string
  description: string
  category: string
  package: string
  icon: string
  tools: string[]
  resources: string[]
  prompts: string[]
  systemPrompt: string
  mcpTools: string[]
  enabled: boolean
  libraryId?: string
}

export interface SkillPackage {
  key: string
  label: string
  skills: Skill[]
  enabledCount: number
}

export interface Guide {
  id: string
  title: string
  url: string
}

export interface GuideSource {
  name: string
  title: string
  url: string
  enabled: boolean
}

export const skillsApi = {
  list(libraryId = '') { return call<Skill[]>(() => App().ListSkills(libraryId)) },
  listEnabled(libraryId = '') { return call<Skill[]>(() => App().ListEnabledSkills(libraryId)) },
  create(skill: Partial<Skill>) { return call(() => App().CreateSkill(skill)) },
  update(name: string, skill: Partial<Skill>) { return call(() => App().UpdateSkill(name, skill)) },
  remove(name: string) { return call(() => App().DeleteSkill(name)) },
  setEnabled(name: string, enabled: boolean) { return call(() => App().SetSkillEnabled(name, enabled)) },
  moveSkill(name: string, newPackage: string) { return call(() => App().MoveSkill(name, newPackage)) },
  reset() { return call(() => App().ResetSkills()) },
  exportAll() { return call<string>(() => App().ExportSkills()) },
  importSkills(data: any) { return call(() => App().ImportSkills(data)) },

  // Market
  listMarket() { return call<any[]>(() => App().ListMarketSkills()) },
  refreshMarket() { return call<any[]>(() => App().RefreshMarketSkills()) },
  installMarket(pkg: any) { return call<any>(() => App().InstallMarketSkill(pkg)) },
  uninstallMarket(name: string) { return call(() => App().UninstallMarketSkill(name)) },

  // Tools
  getEnabledToolNames() { return call<string[]>(() => App().GetEnabledToolNames()) },
  listTools() { return call<any[]>(() => App().ListTools()) },
  listToolsLazy() { return call<any[]>(() => App().ListToolsLazy()) },
  callTool(name: string, args: Record<string, any>) { return call<any>(() => App().CallTool(name, args)) },

  // Guides
  listGuides(query: string) { return call<Guide[]>(() => App().ListGuides(query)) },
  readGuide(id: string) { return call<string>(() => App().ReadGuide(id)) },
  listGuideSources() { return call<GuideSource[]>(() => App().ListGuideSources()) },
  syncGuides() { return call<string[]>(() => App().SyncGuides()) },
  syncOneGuide(name: string) { return call<string>(() => App().SyncOneGuide(name)) },
  addGuideSource(name: string, title: string, url: string, branch: string, sourceType: string) {
    return call(() => App().AddGuideSource(name, title, url, branch, sourceType))
  },
  removeGuideSource(name: string) { return call(() => App().RemoveGuideSource(name)) },
  openGuidesDir() { return call<void>(() => App().OpenGuidesDir()) },
}

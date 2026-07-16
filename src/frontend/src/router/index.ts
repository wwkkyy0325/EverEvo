import { createRouter, createWebHashHistory } from 'vue-router'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/capability' },
    {
      path: '/my-models',
      name: 'my-models',
      component: () => import('@/components/MyModels.vue'),
    },
    {
      path: '/memory',
      name: 'memory',
      component: () => import('@/components/MemoryPanel.vue'),
    },
    {
      path: '/knowledge',
      name: 'knowledge',
      component: () => import('@/components/Knowledge.vue'),
    },
    {
      path: '/paradigm',
      name: 'paradigm',
      component: () => import('@/components/ParadigmLibrary.vue'),
    },
    {
      path: '/guides',
      name: 'guides',
      component: () => import('@/components/GuideCenter.vue'),
    },
    {
      path: '/plugins',
      name: 'plugins',
      component: () => import('@/components/PluginManager.vue'),
    },
    {
      path: '/capability',
      name: 'capability',
      component: () => import('@/components/llm/LLMConfig.vue'),
    },
    {
      path: '/workflow',
      name: 'workflow',
      component: () => import('@/components/WorkflowEditor.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/components/SettingsPanel.vue'),
    },
    {
      path: '/zones',
      name: 'zones',
      component: () => import('@/components/ZonePanel.vue'),
    },
    {
      path: '/collab',
      name: 'collab',
      component: () => import('@/components/CollabPanel.vue'),
    },
    {
      path: '/workbench',
      name: 'workbench',
      component: () => import('@/components/CollabWorkbench.vue'),
    },
    {
      path: '/activity',
      name: 'activity',
      component: () => import('@/components/ActivityHistory.vue'),
    },
  ],
})

export default router

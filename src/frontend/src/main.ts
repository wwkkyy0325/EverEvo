import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import './style.css'
import './styles/components.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)

app.config.errorHandler = (err, _instance, info) => {
  console.error('[Vue error]', info, err)
}

window.addEventListener('unhandledrejection', (event) => {
  console.error('[Unhandled rejection]', event.reason)
})

// Disable browser context menu (right-click) and refresh shortcuts.
// In a Wails desktop app, these can corrupt frontend state because
// a full page reload loses the Wails runtime connection without
// restarting the Go backend.
document.addEventListener('contextmenu', (e) => e.preventDefault())
document.addEventListener('keydown', (e) => {
  // Block F5, Ctrl+R, Ctrl+Shift+R
  if (
    e.key === 'F5' ||
    ((e.ctrlKey || e.metaKey) && e.key === 'r') ||
    ((e.ctrlKey || e.metaKey) && e.key === 'R')
  ) {
    e.preventDefault()
  }
})

app.mount('#app')

/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

// Wails runtime — injected into the WebView at runtime
declare global {
  interface Window {
    go: {
      app: {
        App: Record<string, (...args: any[]) => Promise<any>>
      }
    }
    runtime: {
      EventsOn: (event: string, callback: (...args: any[]) => void) => (() => void) | void
      EventsOff: (event: string, callback?: (...args: any[]) => void) => void
      EventsEmit?: (event: string, data?: any) => void
      BrowserOpenURL: (url: string) => void
      OpenFileDialog: (opts?: any) => Promise<string>
    }
  }
}

export {}

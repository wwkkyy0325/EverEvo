/**
 * Account / Auth API
 */
import { call } from './client'

const App = () => window.go.app.App

export interface AccountInfo {
  source: string
  name: string
  hasToken: boolean
  loginUrl: string
  username: string
  valid: boolean
  reason: string
  verifying: boolean
}

export const accountsApi = {
  list() { return call<AccountInfo[]>(() => App().GetAccounts()) },
  verify(source: string) { return call<any>(() => App().VerifyAccount(source)) },
  openLogin(source: string) { return call(() => App().OpenLoginPage(source)) },
  login(source: string) { return call<string>(() => App().LoginToSource(source)) },
  setToken(source: string, token: string) { return call(() => App().SetAccountToken(source, token)) },
}

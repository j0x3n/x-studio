import { useState, type FormEvent } from 'react'
import { Gem } from 'lucide-react'
import { api } from '../lib/api'

export function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      await api('/api/login', { body: { username, password } })
      const next = new URLSearchParams(location.search).get('next')
      location.assign(next && next.startsWith('/') && !next.startsWith('//') ? next : '/')
    } catch (e) {
      setErr(e instanceof Error ? e.message : '登录失败')
    } finally { setBusy(false) }
  }
  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <div className="brand-row">
          <span className="rail-logo"><Gem /></span>
          <div><h1>X Studio</h1><div className="fs-12 t3">商品、图片、文案和出图</div></div>
        </div>
        <label className="form-field"><span className="label">账号</span>
          <input className="input" autoFocus autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} />
        </label>
        <label className="form-field"><span className="label">密码</span>
          <input className="input" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} />
        </label>
        <div className="form-err">{err}</div>
        <button className="btn btn-primary btn-block" disabled={busy || !username || !password}>{busy ? '登录中…' : '登录'}</button>
      </form>
    </div>
  )
}

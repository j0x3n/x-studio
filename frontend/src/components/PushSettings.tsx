import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { releasePushSubscription, useNotify } from '../lib/notify'
import { errText, useToast } from '../lib/toast'

type Phase = 'loading' | 'unsupported' | 'off' | 'stale' | 'denied' | 'ready' | 'on'

function b64urlToBytes(s: string) {
  const pad = '='.repeat((4 - (s.length % 4)) % 4)
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/') + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

const copy: Record<Phase, { desc: string; help: string }> = {
  loading: { desc: '正在查看这个浏览器能不能接收推送', help: '只有你自己点启用，才会向浏览器要通知权限。' },
  unsupported: { desc: '这个浏览器不支持，或当前页面不是安全地址', help: '关掉页面后的提醒需要 https，或者用本机地址打开。网页开着时，原来的桌面提醒还可以用。' },
  off: { desc: '服务器尚未启用推送', help: '启用后可接收离线提醒，网页开着时仍可使用桌面通知。' },
  stale: { desc: '这个浏览器还留着旧订阅', help: '请取消旧订阅，再为当前账号启用。' },
  denied: { desc: '浏览器拦截了通知权限', help: '要在地址栏左边的网站设置里允许通知，然后再回来启用。' },
  ready: { desc: '这个浏览器还没启用', help: '点启用后才会向浏览器要权限。提醒只有一句「有新的工作通知」，不带具体内容。退出账号时会自动取消。' },
  on: { desc: '这个浏览器已启用，关掉页面也能收到', help: '提醒只有一句「有新的工作通知」。点开会回到这个网站。退出账号时会自动取消，避免和别人共用浏览器时串到你的提醒。' },
}

export function PushSettings() {
  const toast = useToast()
  const { isAdmin, me } = useAuth()
  const { enableDesktop } = useNotify()
  const [phase, setPhase] = useState<Phase>('loading')
  const [serverOn, setServerOn] = useState(false)
  const [publicKey, setPublicKey] = useState('')
  const [subject, setSubject] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let dead = false
    const support = window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && typeof Notification !== 'undefined'
    if (!support) {
      setPhase('unsupported')
      return
    }
    api<{ enabled?: boolean; public_key?: string }>('/api/push/key').then(async key => {
      if (dead) return
      const reg = await navigator.serviceWorker.getRegistration()
      const sub = reg ? await reg.pushManager.getSubscription() : null
      if (dead) return
      const on = !!key.enabled && !!key.public_key
      setServerOn(on)
      if (!on) {
        setPhase(sub ? 'stale' : 'off')
        return
      }
      setPublicKey(key.public_key || '')
      if (sub) setPhase(localStorage.getItem('xs-push-owner') === String(me?.user.id) ? 'on' : 'stale')
      else if (Notification.permission === 'denied') setPhase('denied')
      else setPhase('ready')
    }).catch(e => {
      if (dead) return
      setPhase('off')
      toast(errText(e), 'error')
    })
    return () => { dead = true }
  }, [toast, me?.user.id])

  const generate = async () => {
    setBusy(true)
    try {
      const r = await api<{ enabled?: boolean; public_key?: string }>('/api/push/vapid', { body: { subject: subject.trim() } })
      if (!r.enabled || !r.public_key) {
        setServerOn(false)
        setPhase('off')
        toast('服务器没有启用推送', 'error')
        return
      }
      setPublicKey(r.public_key)
      setServerOn(true)
      setPhase(Notification.permission === 'denied' ? 'denied' : 'ready')
      toast('服务器推送已启用', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }

  const turnOn = async () => {
    if (!publicKey) return
    setBusy(true)
    let newlyCreated: PushSubscription | null = null
    try {
      await enableDesktop()
      const perm = Notification.permission
      if (perm !== 'granted') {
        setPhase(perm === 'denied' ? 'denied' : 'ready')
        toast(perm === 'denied' ? '浏览器拦截了通知权限' : '还没有允许通知', 'error')
        return
      }
      const reg = await navigator.serviceWorker.register('/push-sw.js', { scope: '/' })
      await navigator.serviceWorker.ready
      let sub = await reg.pushManager.getSubscription()
      if (sub && localStorage.getItem('xs-push-owner') !== String(me?.user.id)) { await sub.unsubscribe(); sub = null }
      if (!sub) {
        sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64urlToBytes(publicKey) })
        newlyCreated = sub
      }
      const json = sub.toJSON()
      if (!json.keys?.p256dh || !json.keys.auth) throw new Error('浏览器没有给出订阅密钥')
      await api('/api/push/subscriptions', { body: { endpoint: sub.endpoint, keys: { p256dh: json.keys.p256dh, auth: json.keys.auth } } })
      localStorage.setItem('xs-push-owner', String(me?.user.id))
      setPhase('on')
      toast('这个浏览器关掉页面后也会收到提醒', 'ok')
    } catch (e) {
      if (newlyCreated) await newlyCreated.unsubscribe().catch(() => false)
      toast(errText(e), 'error')
    } finally { setBusy(false) }
  }

  const turnOff = async () => {
    setBusy(true)
    try {
      await releasePushSubscription()
      setPhase(serverOn ? (Notification.permission === 'denied' ? 'denied' : 'ready') : 'off')
      toast('已取消这个浏览器的推送', 'ok')
    } catch (e) { toast(errText(e), 'error') } finally { setBusy(false) }
  }

  const text = copy[phase]
  return (
    <div>
      <div className="setting">
        <div className="grow">
          <div className="title">关掉页面也能收到</div>
          <div className="desc">{text.desc}</div>
        </div>
        {(phase === 'on' || phase === 'stale') && <button className="btn btn-secondary btn-sm" disabled={busy} onClick={turnOff}>取消</button>}
        {phase === 'ready' && <button className="btn btn-secondary btn-sm" disabled={busy || !publicKey} onClick={turnOn}>启用</button>}
      </div>
      {phase === 'off' && isAdmin && (
        <div className="row">
          <input className="input" style={{ width: 280, maxWidth: '100%' }} placeholder="mailto:you@example.com" aria-label="推送联系地址" value={subject} onChange={e => setSubject(e.target.value)} />
          <button className="btn btn-secondary btn-sm" disabled={busy || !subject.trim()} onClick={generate}>启用推送服务</button>
        </div>
      )}
      <div className="help">{text.help}</div>
    </div>
  )
}

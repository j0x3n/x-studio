function safePath(raw) {
  if (typeof raw !== 'string' || raw.length > 200 || raw.charAt(0) !== '/' || raw.charAt(1) === '/' || raw.indexOf('\\') >= 0 || raw.indexOf('..') >= 0 || raw.indexOf('?') >= 0 || raw.indexOf('#') >= 0 || raw.indexOf('@') >= 0 || raw.indexOf(':') >= 0) return '/'
  try {
    var u = new URL(raw, self.location.origin)
    if (u.origin !== self.location.origin || u.pathname !== raw) return '/'
    return u.pathname
  } catch (e) {
    return '/'
  }
}

self.addEventListener('push', function (event) {
  var path = '/'
  try {
    if (event.data) {
      var data = event.data.json()
      if (data && typeof data.url === 'string') path = safePath(data.url)
    }
  } catch (e) {
    path = '/'
  }
  event.waitUntil(self.registration.showNotification('有新的工作通知', {
    body: '打开应用查看',
    tag: 'xs-push',
    renotify: true,
    data: { url: path }
  }))
})

self.addEventListener('notificationclick', function (event) {
  event.notification.close()
  var path = safePath(event.notification.data && event.notification.data.url)
  event.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
    var i
    for (i = 0; i < list.length; i++) {
      if (new URL(list[i].url).origin !== self.location.origin) continue
      var client = list[i]
      var focus = client.focus ? client.focus() : Promise.resolve()
      var nav = client.navigate ? client.navigate(path).catch(function () { return null }) : Promise.resolve()
      return Promise.all([focus, nav])
    }
    if (self.clients.openWindow) return self.clients.openWindow(path)
  }))
})

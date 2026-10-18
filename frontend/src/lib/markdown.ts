// 知识库用的小型 Markdown 渲染：先把所有文字转义，再按常用语法拼 HTML，不会放进任何原始 HTML。
// 支持：# 到 #### 标题、段落、- / 1. 列表（缩进两格嵌套一层）、> 引用、``` 代码块、| 表格 |、---、
// **粗体**、*斜体*、`代码`、[链接](url)、![图片](url)（.mp4 / .webm 渲染成视频）。

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

// 只放行站内相对地址和 http(s)
const safeUrl = (u: string) => {
  const t = u.trim()
  return /^(https?:\/\/|\/(?!\/)|#)/i.test(t) ? t : ''
}

function inline(src: string): string {
  // 先抽出行内代码，避免里面的符号被当成格式
  const codes: string[] = []
  let s = src.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000` })
  s = esc(s)
  s = s.replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (_, alt, url) => {
    const u = safeUrl(url.replace(/&amp;/g, '&'))
    if (!u) return alt
    if (/\.(mp4|webm)(\?|$)/i.test(u)) return `<video src="${esc(u)}" controls preload="metadata"></video>`
    return `<img src="${esc(u)}" alt="${alt}" loading="lazy">`
  })
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, text, url) => {
    const u = safeUrl(url.replace(/&amp;/g, '&'))
    if (!u) return text
    const ext = /^https?:/i.test(u) ? ' target="_blank" rel="noreferrer"' : ''
    return `<a href="${esc(u)}"${ext}>${text}</a>`
  })
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>').replace(/(^|[^*])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>')
  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${esc(codes[Number(i)])}</code>`)
}

const cells = (line: string) => line.trim().replace(/^\||\|$/g, '').split('|').map(c => c.trim())

export function renderMarkdown(md: string): string {
  const lines = (md || '').replace(/\r\n?/g, '\n').split('\n')
  const out: string[] = []
  let i = 0
  const isBlockStart = (l: string) => /^(#{1,4}\s|```|>|\s*([-*]|\d+\.)\s|---+\s*$|\|)/.test(l)
  while (i < lines.length) {
    const line = lines[i]
    if (!line.trim()) { i++; continue }
    let m: RegExpMatchArray | null
    if (line.startsWith('```')) {
      const buf: string[] = []
      i++
      while (i < lines.length && !lines[i].startsWith('```')) buf.push(lines[i++])
      i++
      out.push(`<pre><code>${esc(buf.join('\n'))}</code></pre>`)
    } else if ((m = line.match(/^(#{1,4})\s+(.*)$/))) {
      const n = Math.min(m[1].length + 1, 5) // 正文里的 # 从 h2 开始，标题本身是 h1
      out.push(`<h${n}>${inline(m[2])}</h${n}>`)
      i++
    } else if (/^---+\s*$/.test(line)) {
      out.push('<hr>')
      i++
    } else if (line.startsWith('>')) {
      const buf: string[] = []
      while (i < lines.length && lines[i].startsWith('>')) buf.push(lines[i++].replace(/^>\s?/, ''))
      out.push(`<blockquote>${renderMarkdown(buf.join('\n'))}</blockquote>`)
    } else if (line.trim().startsWith('|') && i + 1 < lines.length && /^\s*\|?\s*:?-{2,}/.test(lines[i + 1])) {
      const head = cells(line)
      i += 2
      const rows: string[][] = []
      while (i < lines.length && lines[i].trim().startsWith('|')) rows.push(cells(lines[i++]))
      out.push(`<table><thead><tr>${head.map(c => `<th>${inline(c)}</th>`).join('')}</tr></thead><tbody>${rows.map(r => `<tr>${head.map((_, j) => `<td>${inline(r[j] || '')}</td>`).join('')}</tr>`).join('')}</tbody></table>`)
    } else if (/^\s*([-*]|\d+\.)\s/.test(line)) {
      const ordered = /^\s*\d+\./.test(line)
      const items: string[] = []
      while (i < lines.length && /^\s*([-*]|\d+\.)\s/.test(lines[i]) && !/^\s{2,}/.test(lines[i])) {
        let item = inline(lines[i].replace(/^\s*([-*]|\d+\.)\s+/, ''))
        i++
        const sub: string[] = []
        while (i < lines.length && /^\s{2,}([-*]|\d+\.)\s/.test(lines[i])) sub.push(lines[i++].trimStart())
        if (sub.length) item += renderMarkdown(sub.join('\n'))
        items.push(`<li>${item}</li>`)
      }
      const tag = ordered ? 'ol' : 'ul'
      out.push(`<${tag}>${items.join('')}</${tag}>`)
    } else {
      const buf: string[] = []
      while (i < lines.length && lines[i].trim() && !(buf.length && isBlockStart(lines[i]))) buf.push(lines[i++])
      out.push(`<p>${buf.map(inline).join('<br>')}</p>`)
    }
  }
  return out.join('\n')
}

// 去掉 Markdown 符号的纯文字（列表摘要用）
export const plainText = (md: string) => (md || '')
  .replace(/```[\s\S]*?```/g, ' ').replace(/!\[[^\]]*\]\([^)]*\)/g, ' ').replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
  .replace(/[#>*`|_-]+/g, ' ').replace(/\s+/g, ' ').trim()

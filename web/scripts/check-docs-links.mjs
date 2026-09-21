#!/usr/bin/env node
/**
 * 校验 web/src/utils/docs.ts 里所有 frp 官方文档链接。
 *
 * 为什么需要它：官网会重构目录结构（2026-03-30 前后把扁平的 `/docs/features/<name>/`
 * 拆成了 `/docs/features/<分类>/<页>/`），旧的四个地址直接 404，而面板里的 `?` 角标
 * 对新窗口打开，失效时没有任何提示 —— 只能靠人点到才发现。
 *
 * 校验内容：HTTP 状态码为 200，且带锚点时该 id 在页面 HTML 中存在。
 * 用法：node scripts/check-docs-links.mjs   （或 npm run check:docs）
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const DOCS_TS = path.join(here, '..', 'src', 'utils', 'docs.ts')
const BASE = 'https://gofrp.org/zh-cn/docs'
const TIMEOUT_MS = 30000

const src = fs.readFileSync(DOCS_TS, 'utf8')
const paths = [...new Set([...src.matchAll(/\$\{BASE\}([^`'")\s]*)/g)].map((m) => m[1]))]

if (paths.length === 0) {
  console.error('未从 docs.ts 中解析到任何链接，请检查文件格式')
  process.exit(1)
}

const pageCache = new Map()
async function getPage(p) {
  if (!pageCache.has(p)) {
    pageCache.set(
      p,
      (async () => {
        const res = await fetch(BASE + p, {
          redirect: 'follow',
          signal: AbortSignal.timeout(TIMEOUT_MS),
        })
        return { status: res.status, html: await res.text() }
      })(),
    )
  }
  return pageCache.get(p)
}

const failures = []
for (const raw of paths) {
  const [p, anchor] = raw.split('#')
  let status = 0
  let anchorOk = null
  try {
    const page = await getPage(p)
    status = page.status
    if (anchor) anchorOk = page.html.includes(`id="${anchor}"`)
  } catch (e) {
    status = -1
    failures.push(`${raw}  → 请求失败：${e.message}`)
    continue
  }
  const ok = status === 200 && anchorOk !== false
  if (!ok) {
    failures.push(`${raw}  → HTTP ${status}${anchorOk === false ? '，锚点 id 不存在' : ''}`)
  }
  console.log(
    `${String(status).padStart(4)}  ${(anchor ? (anchorOk ? '锚点OK' : '锚点缺失') : '-').padEnd(8)}  ${raw}`,
  )
}

console.log(`\n共 ${paths.length} 条链接，独立页面 ${pageCache.size} 个`)
if (failures.length) {
  console.error(`\n❌ 失效 ${failures.length} 条：`)
  failures.forEach((f) => console.error('  ' + f))
  console.error('\n请到 https://gofrp.org/zh-cn/sitemap.xml 查真实路径后修正 docs.ts')
  process.exit(1)
}
console.log('✅ 全部通过：状态 200 且锚点存在')

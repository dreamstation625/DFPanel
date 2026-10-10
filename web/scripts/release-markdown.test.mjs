import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { renderReleaseMarkdown } from '../src/utils/releaseMarkdown.ts'

test('升级提醒和更新列表可读，链接在新窗口打开', () => {
  const notes = readFileSync(new URL('../../docs/release-notes/0.0.3.md', import.meta.url), 'utf8')
  const html = renderReleaseMarkdown(notes + '\n[Release](https://github.com/dreamstation625/DFPanel/releases)')
  assert.match(html, /class="release-alert release-alert--warning"/)
  assert.match(html, /升级提醒/)
  assert.match(html, /<strong>本版本必须同步升级面板和所有 Agent。<\/strong>/)
  assert.match(html, /<ul>/)
  assert.match(html, /target="_blank" rel="noopener noreferrer"/)
  assert.doesNotMatch(html, /\[!WARNING\]/)
})

test('不执行 Release 中的 HTML 和危险链接，也不加载远程图片', () => {
  const html = renderReleaseMarkdown('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[点击](javascript:alert(1))\n\n[点击](data:text/html;base64,eA==)\n\n![<img onerror=x>](https://example.test/tracking.png)')
  assert.doesNotMatch(html, /<script|<img|href="javascript:|href="data:/i)
  assert.match(html, /&lt;script&gt;/)
  assert.match(html, /&lt;img onerror=x&gt;/)
  assert.doesNotMatch(html, /tracking\.png/)
})

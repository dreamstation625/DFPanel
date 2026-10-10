import MarkdownIt from 'markdown-it'

// Release 内容来自外部：禁用原始 HTML，保留解析器对危险链接的校验。
const markdown = new MarkdownIt({ html: false, linkify: false })

const alertLabels: Record<string, string> = {
  NOTE: '说明', TIP: '提示', IMPORTANT: '重要说明', WARNING: '升级提醒', CAUTION: '注意',
}

// 识别 GitHub 的提示块，让升级提醒在面板中也能显示出来。
markdown.core.ruler.after('inline', 'release_alerts', (state) => {
  for (let i = 0; i < state.tokens.length; i++) {
    const token = state.tokens[i]
    const inline = state.tokens[i + 2]
    if (token?.type !== 'blockquote_open' || state.tokens[i + 1]?.type !== 'paragraph_open' || inline?.type !== 'inline') continue
    const marker = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\](?:\n|$)/.exec(inline.content)
    if (!marker) continue
    const kind = marker[1]!
    token.attrJoin('class', `release-alert release-alert--${kind.toLowerCase()}`)
    inline.content = inline.content.slice(marker[0].length)
    inline.children = []
    state.md.inline.parse(inline.content, state.md, state.env, inline.children)
    const title = new state.Token('release_alert_title', '', 0)
    title.content = alertLabels[kind]!
    state.tokens.splice(i + 1, inline.content ? 0 : 3, title)
  }
})

markdown.renderer.rules.release_alert_title = (tokens, index) =>
  `<div class="release-alert-title">${markdown.utils.escapeHtml(tokens[index]!.content)}</div>`

markdown.renderer.rules.link_open = (tokens, index, options, _env, renderer) => {
  tokens[index]!.attrSet('target', '_blank')
  tokens[index]!.attrSet('rel', 'noopener noreferrer')
  return renderer.renderToken(tokens, index, options)
}

// 展示图片说明，不在打开弹窗时加载 Release 中的远程图片。
markdown.renderer.rules.image = (tokens, index, options, env, renderer) => {
  const token = tokens[index]!
  const alt = renderer.renderInlineAsText(token.children || [], options, env)
  return `<span class="release-image">${markdown.utils.escapeHtml(alt || '图片')}</span>`
}

export function renderReleaseMarkdown(content: string): string {
  return markdown.render(content)
}

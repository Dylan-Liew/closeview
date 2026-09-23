import assert from 'node:assert/strict'
import { test } from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createServer } from 'vite'

test('message markdown renders GFM without executable HTML or remote images', async () => {
  const vite = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  try {
    const { MarkdownContent } = await vite.ssrLoadModule('/src/components/markdown.tsx')
    const render = text => renderToStaticMarkup(React.createElement(MarkdownContent, { text }))

    const table = render('| Name | Value |\n| --- | --- |\n| **bold** | `code` |')
    assert.match(table, /<table>/)
    assert.match(table, /<strong>bold<\/strong>/)
    assert.match(table, /<code>code<\/code>/)

    const unsafe = render('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n![remote](https://example.com/pixel)\n\n[bad](javascript:alert(1)) [data](data:text/html,hi) [safe](https://example.com)')
    assert.doesNotMatch(unsafe, /<script|<img|onerror|javascript:|data:text\/html/)
    assert.match(unsafe, /<a href="">bad<\/a>/)
    assert.match(unsafe, /href="https:\/\/example.com"/)

    const unsafeTable = render('| Link | Image |\n| --- | --- |\n| [bad](javascript:alert(1)) | ![remote](https://example.com/pixel) |')
    assert.match(unsafeTable, /<table>/)
    assert.doesNotMatch(unsafeTable, /<img|javascript:/)

    const code = render('```html\n<img src=x onerror=alert(1)>\n```')
    assert.match(code, /&lt;img src=x onerror=alert\(1\)&gt;/)
    assert.doesNotMatch(code, /<img/)
  } finally {
    await vite.close()
  }
})

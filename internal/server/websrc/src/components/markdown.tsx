import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

export function MarkdownContent({ text }: { text: string }) {
  return (
    <Markdown
      remarkPlugins={[remarkGfm]}
      skipHtml
      disallowedElements={['img']}
      components={{
        table({ node: _node, ...props }) {
          return <div className="markdown-table-wrap"><table {...props} /></div>
        },
      }}
    >{text}</Markdown>
  )
}

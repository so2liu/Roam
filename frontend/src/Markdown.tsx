// 对话文本的 Markdown 渲染（Claude / Codex 共用）。深色主题、紧凑边距以贴合气泡。
// 工具输出/命令仍按原样 <pre> 显示，不走这里。
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { CSSProperties } from 'react'

const mono = 'ui-monospace, SFMono-Regular, Menlo, monospace'

const preStyle: CSSProperties = {
  margin: '6px 0', padding: 10, borderRadius: 6, background: '#0d1117', border: '1px solid #21262d',
  overflow: 'auto', fontFamily: mono, fontSize: 12.5, lineHeight: 1.45,
}
const inlineCode: CSSProperties = {
  fontFamily: mono, fontSize: '0.88em', background: 'rgba(110,118,129,.28)',
  padding: '1px 5px', borderRadius: 4,
}

export default function Markdown({ children, accent = '#58a6ff' }: { children: string; accent?: string }) {
  return (
    <div style={{ fontSize: 13.5, lineHeight: 1.55, wordBreak: 'break-word' }}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // 段落/列表/标题 收紧默认大边距
          p: ({ children }) => <p style={{ margin: '4px 0' }}>{children}</p>,
          ul: ({ children }) => <ul style={{ margin: '4px 0', paddingLeft: 20 }}>{children}</ul>,
          ol: ({ children }) => <ol style={{ margin: '4px 0', paddingLeft: 20 }}>{children}</ol>,
          li: ({ children }) => <li style={{ margin: '2px 0' }}>{children}</li>,
          h1: ({ children }) => <h1 style={{ fontSize: 18, margin: '8px 0 4px', fontWeight: 700 }}>{children}</h1>,
          h2: ({ children }) => <h2 style={{ fontSize: 16, margin: '8px 0 4px', fontWeight: 700 }}>{children}</h2>,
          h3: ({ children }) => <h3 style={{ fontSize: 14.5, margin: '6px 0 4px', fontWeight: 600 }}>{children}</h3>,
          h4: ({ children }) => <h4 style={{ fontSize: 13.5, margin: '6px 0 4px', fontWeight: 600 }}>{children}</h4>,
          a: ({ children, href }) => <a href={href} target="_blank" rel="noreferrer" style={{ color: accent, textDecoration: 'underline' }}>{children}</a>,
          blockquote: ({ children }) => <blockquote style={{ margin: '6px 0', padding: '2px 10px', borderLeft: '3px solid #30363d', color: '#aab2bd' }}>{children}</blockquote>,
          hr: () => <hr style={{ border: 0, borderTop: '1px solid #30363d', margin: '8px 0' }} />,
          // pre 透传，由 code 统一加样式（块级 vs 行内）
          pre: ({ children }) => <>{children}</>,
          code: ({ className, children }) => {
            const text = String(children)
            const block = (className && className.startsWith('language-')) || text.includes('\n')
            if (block) return <pre style={preStyle}><code style={{ fontFamily: mono }}>{children}</code></pre>
            return <code style={inlineCode}>{children}</code>
          },
          table: ({ children }) => <table style={{ borderCollapse: 'collapse', margin: '6px 0', fontSize: 12.5 }}>{children}</table>,
          th: ({ children }) => <th style={{ border: '1px solid #30363d', padding: '3px 8px', textAlign: 'left', background: '#161b22' }}>{children}</th>,
          td: ({ children }) => <td style={{ border: '1px solid #30363d', padding: '3px 8px' }}>{children}</td>,
          img: ({ src, alt }) => <img src={src} alt={alt} style={{ maxWidth: '100%', borderRadius: 6 }} />,
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}

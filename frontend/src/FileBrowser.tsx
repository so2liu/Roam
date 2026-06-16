// 文件侧栏 —— 在 Claude / Codex 对话页右侧浏览工作目录、查看文件内容（类似 codex 右侧边栏）。
// 单层可导航列表：目录在前可进入、↑ 回上级、点文件在弹层里查看正文。
import { useEffect, useState } from 'react'
import { Button, Modal, Spin } from 'antd'
import { api } from './api'
import Markdown from './Markdown'

const IMG_EXT = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'ico', 'avif', 'svg']
const MD_EXT = ['md', 'markdown', 'mdx']
function extOf(path: string): string {
  const m = path.toLowerCase().match(/\.([a-z0-9]+)$/)
  return m ? m[1] : ''
}

interface Entry { name: string; dir: boolean; size: number }
interface Dir { path: string; parent: string; entries: Entry[] }

function fmtSize(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' K'
  return (n / 1024 / 1024).toFixed(1) + ' M'
}

// 目录/文件图标（线性）
const FolderIcon = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>
)
const FileIcon = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><polyline points="14 3 14 8 19 8" /></svg>
)

function Viewer({ path, accent, onClose }: { path: string; accent: string; onClose: () => void }) {
  const ext = extOf(path)
  const isImg = IMG_EXT.includes(ext)
  const isMd = MD_EXT.includes(ext)
  const rawUrl = `/api/file/raw?path=${encodeURIComponent(path)}`
  const [data, setData] = useState<any>(null)
  const [err, setErr] = useState('')
  const [source, setSource] = useState(false) // markdown：源码/渲染切换

  useEffect(() => {
    if (isImg) return // 图片直接走 <img>，不取文本
    setData(null); setErr(''); setSource(false)
    api('GET', `/file?path=${encodeURIComponent(path)}`).then((r) => setData(r.data)).catch((e) => setErr(e.message))
  }, [path, isImg])

  const name = path.split('/').pop()
  const codePre = (text: string) => (
    <pre style={{ margin: 0, whiteSpace: 'pre', overflow: 'auto', maxHeight: '70vh', background: '#0d1117', padding: 12, borderRadius: 8, fontFamily: 'ui-monospace, monospace', fontSize: 12.5, lineHeight: 1.5, color: '#c9d1d9' }}>{text}</pre>
  )

  return (
    <Modal open onCancel={onClose} footer={null} width="min(900px,94vw)"
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, paddingRight: 28 }}>
          <span style={{ fontFamily: 'ui-monospace, monospace', fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            <span style={{ color: accent }}>▸</span> {name}
          </span>
          <span style={{ flex: 1 }} />
          {isMd && data && !data.binary && (
            <Button size="small" onClick={() => setSource((s) => !s)}>{source ? '渲染' : '源码'}</Button>
          )}
          <a href={rawUrl} target="_blank" rel="noreferrer" style={{ color: '#8b949e', fontSize: 12 }}>↗ 原始</a>
        </div>
      }>
      {isImg ? (
        <div style={{ textAlign: 'center', background: '#0d1117', borderRadius: 8, padding: 12 }}>
          <img src={rawUrl} alt={name} style={{ maxWidth: '100%', maxHeight: '74vh', objectFit: 'contain' }} />
        </div>
      ) : (
        <>
          {err && <div style={{ color: '#f85149' }}>{err}</div>}
          {!data && !err && <div style={{ textAlign: 'center', padding: 30 }}><Spin /></div>}
          {data && data.binary && (
            <div style={{ color: '#8b949e' }}>二进制文件，无法预览（{fmtSize(data.size)}）。<a href={rawUrl} target="_blank" rel="noreferrer" style={{ color: accent }}>下载/打开原始文件</a></div>
          )}
          {data && !data.binary && (
            <>
              {isMd && !source
                ? <div style={{ maxHeight: '70vh', overflow: 'auto' }}><Markdown accent={accent}>{data.content}</Markdown></div>
                : codePre(data.content)}
              {data.truncated && <div style={{ color: '#d29922', fontSize: 12, marginTop: 6 }}>⚠ 文件较大，仅显示前 512 KB</div>}
            </>
          )}
        </>
      )}
    </Modal>
  )
}

export default function FileBrowser({ dir, accent = '#58a6ff', onClose }: { dir?: string; accent?: string; onClose?: () => void }) {
  const [path, setPath] = useState(dir || '')
  const [data, setData] = useState<Dir | null>(null)
  const [err, setErr] = useState('')
  const [view, setView] = useState<string | null>(null)

  // 会话切换（dir 变化）→ 回到工作目录根
  useEffect(() => { setPath(dir || '') }, [dir])

  useEffect(() => {
    let stop = false
    setErr('')
    const q = path ? `?path=${encodeURIComponent(path)}` : ''
    api('GET', `/files${q}`).then((r) => { if (!stop) setData(r.data) }).catch((e) => { if (!stop) setErr(e.message) })
    return () => { stop = true }
  }, [path])

  const cur = data?.path || path
  // 根目录之上不再回退（防止越过工作目录乱逛；dir 为空时允许一直向上）
  const canUp = !!data && data.parent !== data.path && (!dir || cur !== dir)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', background: '#0a0e13', borderLeft: '1px solid #21262d' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 10px', borderBottom: '1px solid #21262d' }}>
        <span style={{ color: accent }}><FolderIcon /></span>
        <span style={{ color: '#e6edf3', fontWeight: 600, fontSize: 13 }}>文件</span>
        <span style={{ flex: 1 }} />
        {onClose && <a onClick={onClose} style={{ color: '#8b949e', fontSize: 12 }}>✕</a>}
      </div>
      <div title={cur} style={{ padding: '4px 10px', color: '#8b949e', fontSize: 11.5, fontFamily: 'ui-monospace, monospace', borderBottom: '1px solid #161b22', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', direction: 'rtl', textAlign: 'left' }}>{cur || '…'}</div>
      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '4px 0' }}>
        {err && <div style={{ color: '#f85149', fontSize: 12, padding: '6px 10px' }}>{err}</div>}
        {canUp && (
          <div onClick={() => setPath(data!.parent)} style={rowStyle()}>
            <span style={{ color: '#8b949e' }}>↑</span><span style={{ color: '#8b949e' }}>上级目录</span>
          </div>
        )}
        {data?.entries.map((e) => (
          <div key={e.name} onClick={() => (e.dir ? setPath((cur === '/' ? '' : cur) + '/' + e.name) : setView((cur === '/' ? '' : cur) + '/' + e.name))} style={rowStyle()}>
            <span style={{ color: e.dir ? accent : '#6e7681', flex: '0 0 auto', display: 'inline-flex' }}>{e.dir ? <FolderIcon /> : <FileIcon />}</span>
            <span style={{ color: e.dir ? '#e6edf3' : '#c9d1d9', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{e.name}</span>
            {!e.dir && <span style={{ color: '#6e7681', fontSize: 11, flex: '0 0 auto' }}>{fmtSize(e.size)}</span>}
          </div>
        ))}
        {data && data.entries.length === 0 && <div style={{ color: '#6e7681', fontSize: 12, padding: '6px 10px' }}>空目录</div>}
      </div>
      {view && <Viewer path={view} accent={accent} onClose={() => setView(null)} />}
    </div>
  )
}

function rowStyle(): React.CSSProperties {
  return { display: 'flex', alignItems: 'center', gap: 8, padding: '5px 10px', cursor: 'pointer', fontSize: 13, userSelect: 'none' }
}

import { useEffect, useState, type ReactNode } from 'react'
import { IconAlertCircle, IconMenu2, IconSparkles, IconTerminal2 } from '@tabler/icons-react'
import { MarkdownContent } from './markdown'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Skeleton } from './ui/skeleton'
import { sourceIcon, sourceLabel } from '../lib/source'
import type { MCPCatalog, SkillCatalog, SkillDetail } from '../lib/types'
import { cn } from '../lib/utils'

export function SkillsSidebar({ catalog, activeID, loading, filtered, onSelect }: {
  catalog: SkillCatalog
  activeID: string
  loading: boolean
  filtered: boolean
  onSelect: (id: string) => void
}) {
  if (loading) return <LibraryListSkeleton />
  if (!catalog.skills.length) {
    return (
      <div className="sidebar-empty">
        <IconSparkles size={22} />
        <span>{filtered ? 'No matching skills' : 'No local skills found'}</span>
      </div>
    )
  }
  return (
    <div className="library-list">
      {catalog.skills.map(skill => (
        <button
          key={skill.id}
          className={cn('library-row', skill.id === activeID && 'active')}
          onClick={() => onSelect(skill.id)}
        >
          <div className={cn('source-icon', `source-${skill.source}`)}>{sourceIcon(skill.source, 15)}</div>
          <div className="min-w-0 flex-1">
            <div className="library-row-top">
              <strong>{skill.name}</strong>
              <span>{skill.scope}</span>
            </div>
            {skill.description && <p>{skill.description}</p>}
          </div>
        </button>
      ))}
    </div>
  )
}

export function SkillsMain({ skillID, onOpenNav }: {
  skillID: string
  onOpenNav: () => void
}) {
  const [detail, setDetail] = useState<SkillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!skillID) {
      setDetail(null)
      setLoading(false)
      return
    }
    const controller = new AbortController()
    setLoading(true)
    fetch(`/api/skills/${encodeURIComponent(skillID)}`, { cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const data = await response.json()
        if (!response.ok) throw new Error(data.error || 'Could not load skill')
        return data as SkillDetail
      })
      .then(data => {
        setDetail(data)
        setError('')
      })
      .catch(reason => {
        if (reason instanceof DOMException && reason.name === 'AbortError') return
        setDetail(null)
        setError(reason instanceof Error ? reason.message : 'Could not load skill')
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [skillID])

  if (loading) return <DocumentSkeleton />
  if (error) {
    return <div className="main-empty"><div className="empty-icon"><IconAlertCircle size={24} /></div><h1>Could not load skill</h1><p>{error}</p></div>
  }
  if (!detail) {
    return (
      <div className="main-empty">
        <div className="empty-icon"><IconSparkles size={24} /></div>
        <h1>Select a skill</h1>
        <p>Browse installed OpenCode, Codex, and Claude skills.</p>
      </div>
    )
  }

  const skill = detail.skill
  return (
    <>
      <LibraryHeader onOpenNav={onOpenNav}>
        <h1 title={skill.name}>{skill.name}</h1>
        <div className="header-meta">
          <span>{sourceLabel(skill.source)}</span>
          <i />
          <span title={skill.path}>{skill.scope} · {skill.path}</span>
        </div>
        <Badge variant="outline" className={cn('source-badge', `source-${skill.source}`)}>
          {sourceIcon(skill.source, 12)}
          {sourceLabel(skill.source)}
        </Badge>
      </LibraryHeader>
      <div className="library-content">
        <div className="library-document markdown-content">
          <MarkdownContent text={detail.content || 'This skill has no additional instructions.'} />
        </div>
      </div>
    </>
  )
}

export function MCPSidebar({ catalog, activeID, loading, filtered, onSelect }: {
  catalog: MCPCatalog
  activeID: string
  loading: boolean
  filtered: boolean
  onSelect: (id: string) => void
}) {
  if (loading) return <LibraryListSkeleton />
  if (!catalog.servers.length) {
    return (
      <div className="sidebar-empty">
        <IconTerminal2 size={22} />
        <span>{filtered ? 'No matching MCP servers' : 'No MCP servers configured'}</span>
      </div>
    )
  }
  return (
    <div className="library-list">
      {catalog.servers.map(server => (
        <button
          key={server.id}
          className={cn('library-row', server.id === activeID && 'active')}
          onClick={() => onSelect(server.id)}
        >
          <div className={cn('source-icon', `source-${server.source}`)}>{sourceIcon(server.source, 15)}</div>
          <div className="min-w-0 flex-1">
            <div className="library-row-top">
              <strong>{server.name}</strong>
              <span>{server.transport}</span>
            </div>
            <p>{server.command || server.url || 'Transport not recorded'}</p>
          </div>
        </button>
      ))}
    </div>
  )
}

export function MCPMain({ catalog, activeID, onOpenNav }: {
  catalog: MCPCatalog
  activeID: string
  onOpenNav: () => void
}) {
  const server = catalog.servers.find(item => item.id === activeID)
  if (!server) {
    return (
      <div className="main-empty">
        <div className="empty-icon"><IconTerminal2 size={24} /></div>
        <h1>Select an MCP server</h1>
        <p>Inspect local agent MCP configuration with secret values hidden.</p>
      </div>
    )
  }

  return (
    <>
      <LibraryHeader onOpenNav={onOpenNav}>
        <h1 title={server.name}>{server.name}</h1>
        <div className="header-meta">
          <span>{sourceLabel(server.source)}</span>
          <i />
          <span>{server.configFile}</span>
        </div>
        <Badge variant="outline" className={cn('source-badge', `source-${server.source}`)}>
          {sourceIcon(server.source, 12)}
          {sourceLabel(server.source)}
        </Badge>
      </LibraryHeader>
      <div className="library-content">
        <div className="config-stack">
          <section className="config-card">
            <div className="config-card-title">Transport</div>
            <dl>
              <ConfigTerm term="Type" value={server.transport} />
              <ConfigTerm term="Status" value={server.disabled ? 'Disabled' : 'Enabled'} />
              {server.auth && <ConfigTerm term="Authentication" value={server.auth} />}
              {server.url && <ConfigTerm term="URL" value={server.url} mono />}
            </dl>
          </section>

          {(server.command || server.args.length > 0) && (
            <section className="config-card">
              <div className="config-card-title">Local command</div>
              <pre><code>{[server.command, ...server.args].filter(Boolean).join(' ')}</code></pre>
            </section>
          )}

          <section className="config-card">
            <div className="config-card-title">Environment keys</div>
            {server.environment.length ? <ConfigChips values={server.environment} /> : <p className="config-empty">None</p>}
          </section>

          <section className="config-card">
            <div className="config-card-title">Header keys</div>
            {server.headers.length ? <ConfigChips values={server.headers} /> : <p className="config-empty">None</p>}
          </section>

          <p className="privacy-note">Credential values are intentionally omitted from the CloseView API.</p>
        </div>
      </div>
    </>
  )
}

function LibraryHeader({ onOpenNav, children }: {
  onOpenNav: () => void
  children: ReactNode
}) {
  return (
    <header className="session-header">
      <Button aria-label="Open library navigation" variant="ghost" size="icon" className="mobile-session-menu" onClick={onOpenNav}>
        <IconMenu2 size={19} />
      </Button>
      <div className="session-header-info">{children}</div>
    </header>
  )
}

function ConfigTerm({ term, value, mono = false }: {
  term: string
  value: string
  mono?: boolean
}) {
  return <div className="config-term"><dt>{term}</dt><dd className={cn(mono && 'font-mono')}>{value}</dd></div>
}

function ConfigChips({ values }: { values: string[] }) {
  return <div className="config-chips">{values.map(value => <span key={value}>{value}</span>)}</div>
}

function LibraryListSkeleton() {
  return <div className="space-y-2 p-2">{Array.from({ length: 7 }).map((_, index) => <Skeleton key={index} className="h-16 w-full" />)}</div>
}

function DocumentSkeleton() {
  return <div className="library-content"><div className="library-document space-y-5"><Skeleton className="h-7 w-52" /><Skeleton className="h-4 w-full" /><Skeleton className="h-4 w-11/12" /><Skeleton className="h-24 w-full" /><Skeleton className="h-4 w-2/3" /></div></div>
}

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  IconAlertCircle,
  IconBrandOpenai,
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconCode,
  IconCopy,
  IconMenu2,
  IconMessageCircle,
  IconGitBranch,
  IconRefresh,
  IconSearch,
  IconSparkles,
  IconTerminal2,
  IconTrash,
  IconX,
} from '@tabler/icons-react'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { Input } from './components/ui/input'
import { PanelResize } from './components/panel-resize'
import { Skeleton } from './components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from './components/ui/alert-dialog'
import { Tooltip } from './components/ui/tooltip'
import type { Catalog, Message, Session, SessionDetail, SourceName, ToolCall } from './lib/types'
import { cn } from './lib/utils'

const sourceOrder: Array<'all' | SourceName> = ['all', 'opencode', 'codex', 'claude']

export function App() {
  const [catalog, setCatalog] = useState<Catalog>({ sessions: [], sources: [] })
  const [activeID, setActiveID] = useState(() => new URLSearchParams(location.search).get('session') ?? '')
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [query, setQuery] = useState('')
  const [source, setSource] = useState<'all' | SourceName>('all')
  const [loadingCatalog, setLoadingCatalog] = useState(true)
  const [loadingDetail, setLoadingDetail] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [mobilePickerOpen, setMobilePickerOpen] = useState(() => !new URLSearchParams(location.search).get('session'))
  const [error, setError] = useState('')
  const [expandedSessions, setExpandedSessions] = useState<Set<string>>(() => new Set())
  const [detailRevision, setDetailRevision] = useState(0)

  const loadCatalog = useCallback(async (isRefresh = false) => {
    if (isRefresh) setRefreshing(true)
    else setLoadingCatalog(true)
    try {
      const response = await fetch('/api/sessions', { cache: 'no-store' })
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || 'Could not load sessions')
      setCatalog({ sessions: data.sessions ?? [], sources: data.sources ?? [] })
      if (isRefresh) setDetailRevision(value => value + 1)
      setError('')
      setActiveID(current => current || data.sessions?.[0]?.id || '')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not load sessions')
    } finally {
      setLoadingCatalog(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => { void loadCatalog() }, [loadCatalog])

  useEffect(() => {
    if (!activeID) {
      setDetail(null)
      return
    }
    const controller = new AbortController()
    setLoadingDetail(true)
    fetch(`/api/sessions/${encodeURIComponent(activeID)}`, { cache: 'no-store', signal: controller.signal })
      .then(async response => {
        const data = await response.json()
        if (!response.ok) throw new Error(data.error || 'Could not load session')
        return data as SessionDetail
      })
      .then(data => {
        setDetail(data)
        setError('')
        const url = new URL(location.href)
        url.searchParams.set('session', activeID)
        history.replaceState(null, '', url)
      })
      .catch(reason => {
        if (reason instanceof DOMException && reason.name === 'AbortError') return
        setError(reason instanceof Error ? reason.message : 'Could not load session')
      })
      .finally(() => { if (!controller.signal.aborted) setLoadingDetail(false) })
    return () => controller.abort()
  }, [activeID, detailRevision])

  const visibleSessionForest = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    const sessions = catalog.sessions.filter(session => {
      if (source !== 'all' && session.source !== source) return false
      return true
    })
    return filterSessionForest(buildSessionForest(sessions), normalized)
  }, [catalog.sessions, query, source])

  const sourceCounts = useMemo<Record<SourceName, number>>(() => ({
    opencode: catalog.sessions.filter(session => session.source === 'opencode').length,
    codex: catalog.sessions.filter(session => session.source === 'codex').length,
    claude: catalog.sessions.filter(session => session.source === 'claude').length,
  }), [catalog.sessions])
  const visibleSourceTabs = sourceOrder.filter(item => item === 'all' || sourceCounts[item] > 0)

  useEffect(() => {
    if (source !== 'all' && sourceCounts[source] === 0) setSource('all')
  }, [source, sourceCounts])

  useEffect(() => {
    if (!activeID) return
    const parents = new Map(catalog.sessions.map(session => [session.id, session.parentId]))
    setExpandedSessions(current => {
      const next = new Set(current)
      let parentID = parents.get(activeID)
      while (parentID) {
        next.add(parentID)
        parentID = parents.get(parentID)
      }
      return next.size === current.size ? current : next
    })
  }, [activeID, catalog.sessions])

  async function deleteSession() {
    if (!detail) return
    setDeleting(true)
    try {
      const response = await fetch(`/api/sessions/${encodeURIComponent(detail.session.id)}`, {
        method: 'DELETE',
        headers: { 'X-CloseView-Confirm': 'delete-session' },
      })
      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Could not delete session')
      }
      const deletedID = detail.session.id
      const next = detail.session.parentId || catalog.sessions.find(session => session.id !== deletedID)?.id || ''
      setCatalog(current => ({ ...current, sessions: current.sessions.filter(session => session.id !== deletedID) }))
      setDetail(null)
      setActiveID(next)
      if (!next) setMobilePickerOpen(true)
      setDeleteOpen(false)
      const url = new URL(location.href)
      if (next) url.searchParams.set('session', next)
      else url.searchParams.delete('session')
      history.replaceState(null, '', url)
      await loadCatalog(true)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Could not delete session')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className={cn('app-shell', mobilePickerOpen && 'mobile-picker-open')}>
      <aside id="sessions-panel" className={cn('session-sidebar', mobilePickerOpen ? 'mobile-open' : 'mobile-collapsed')}>
        <PanelResize side="left" />
        <div className="brand-row">
          <div className="brand-mark"><img src="/closeview.png" alt="" className="brand-logo" /></div>
          <div>
            <div className="brand-name">CloseView</div>
            <div className="brand-subtitle">Local session history</div>
          </div>
          <Tooltip label="Refresh local sessions">
            <Button aria-label="Refresh local sessions" size="icon" variant="ghost" className="ml-auto size-8" onClick={() => void loadCatalog(true)} disabled={refreshing}>
              <IconRefresh size={16} className={cn(refreshing && 'animate-spin')} />
            </Button>
          </Tooltip>
          {activeID && (
            <Button aria-label="Close session picker" size="icon" variant="ghost" className="mobile-picker-close size-8" onClick={() => setMobilePickerOpen(false)}>
              <IconX size={16} />
            </Button>
          )}
        </div>

        <div className="sidebar-controls">
          <div className="relative">
            <IconSearch className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" size={15} />
            <Input value={query} onChange={event => setQuery(event.target.value)} placeholder="Search sessions" className="pl-8" />
          </div>
          <div className="source-tabs" role="tablist" aria-label="Session source">
            {visibleSourceTabs.map(item => (
              <button key={item} role="tab" aria-selected={source === item} className={cn('source-tab', source === item && 'active')} onClick={() => setSource(item)}>
                {item === 'all' ? 'All' : sourceLabel(item)}
                <span>{item === 'all' ? catalog.sessions.length : sourceCounts[item]}</span>
              </button>
            ))}
          </div>
        </div>

        <div className="session-list">
          {loadingCatalog ? <SessionListSkeleton /> : visibleSessionForest.roots.length || visibleSessionForest.detached.length ? <>
            {visibleSessionForest.roots.map(node => (
              <SessionTree
                key={node.session.id}
                node={node}
                activeID={activeID}
                expanded={expandedSessions}
                forceExpanded={Boolean(query.trim())}
                onToggle={toggleExpanded(setExpandedSessions)}
                onSelect={id => { setActiveID(id); setMobilePickerOpen(false) }}
              />
            ))}
            {visibleSessionForest.detached.length > 0 && (
              <DetachedSessions
                nodes={visibleSessionForest.detached}
                activeID={activeID}
                expanded={expandedSessions}
                forceExpanded={Boolean(query.trim())}
                onToggle={toggleExpanded(setExpandedSessions)}
                onSelect={id => { setActiveID(id); setMobilePickerOpen(false) }}
              />
            )}
          </> : (
            <div className="sidebar-empty">
              <IconSearch size={22} />
              <span>{query ? 'No matching sessions' : 'No local sessions found'}</span>
            </div>
          )}
        </div>

      </aside>

      <button
        aria-label="Close session navigation"
        className={cn('mobile-sidebar-backdrop', mobilePickerOpen && 'open')}
        onClick={() => setMobilePickerOpen(false)}
      />

      <main className="session-main">
        {error && (
          <div className="error-banner"><IconAlertCircle size={16} /><span>{error}</span><button onClick={() => setError('')}>Dismiss</button></div>
        )}
        {loadingDetail ? <DetailSkeleton /> : detail ? (
          <>
            <SessionHeader
              session={detail.session}
              parent={catalog.sessions.find(session => session.id === detail.session.parentId)}
              onOpenNav={() => setMobilePickerOpen(true)}
              onSelectParent={id => setActiveID(id)}
              onDelete={() => setDeleteOpen(true)}
            />
            <Transcript key={detail.session.id} detail={detail} />
          </>
        ) : (
          <div className="main-empty">
            <div className="empty-icon"><IconMessageCircle size={24} /></div>
            <h1>Select a session</h1>
            <p>Browse local OpenCode, Codex, and Claude history.</p>
          </div>
        )}
      </main>

      <aside id="prompts-panel" className="outline-panel">
        <PanelResize side="right" />
        <div className="outline-title">Prompts</div>
        <div className="outline-list">
          {(detail?.messages ?? []).filter(message => message.role === 'user' && message.content.trim()).map((message, index) => (
            <button key={message.id} onClick={() => document.getElementById(message.id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })}>
              <span>{String(index + 1).padStart(2, '0')}</span>
              <strong>{firstLine(message.content)}</strong>
            </button>
          ))}
          {detail && !detail.messages.some(message => message.role === 'user') && <p>No user prompts.</p>}
        </div>
      </aside>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <div className="delete-icon"><IconTrash size={19} /></div>
          <AlertDialogTitle>Delete this {detail?.session.isSubsession ? 'sub-session' : `${detail ? sourceLabel(detail.session.source) : ''} session`}?</AlertDialogTitle>
          <AlertDialogDescription>
            This permanently removes <strong className="text-foreground">{detail?.session.title}</strong> from its native local session store. This cannot be undone by CloseView.
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction disabled={deleting} onClick={event => { event.preventDefault(); void deleteSession() }}>
              {deleting ? <IconRefresh size={15} className="animate-spin" /> : <IconTrash size={15} />}
              {deleting ? 'Deleting…' : 'Delete session'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface SessionNode {
  session: Session
  children: SessionNode[]
}

interface SessionForest {
  roots: SessionNode[]
  detached: SessionNode[]
}

const detachedGroupID = '__detached_subsessions__'

function DetachedSessions({ nodes, activeID, expanded, forceExpanded, onToggle, onSelect }: {
  nodes: SessionNode[]
  activeID: string
  expanded: Set<string>
  forceExpanded: boolean
  onToggle: (id: string) => void
  onSelect: (id: string) => void
}) {
  const isExpanded = forceExpanded || expanded.has(detachedGroupID) || nodes.some(node => sessionTreeContains(node, activeID))
  return (
    <div className="detached-sessions">
      <button className="detached-sessions-toggle" aria-expanded={isExpanded} onClick={() => onToggle(detachedGroupID)}>
        <IconChevronRight size={14} className={cn(isExpanded && 'expanded')} />
        <IconGitBranch size={14} />
        <span>Detached sub-sessions</span>
        <strong>{nodes.length}</strong>
      </button>
      {isExpanded && nodes.map(node => (
        <SessionTree
          key={node.session.id}
          node={node}
          activeID={activeID}
          expanded={expanded}
          forceExpanded={forceExpanded}
          onToggle={onToggle}
          onSelect={onSelect}
          depth={1}
        />
      ))}
    </div>
  )
}

function SessionTree({ node, activeID, expanded, forceExpanded, onToggle, onSelect, depth = 0 }: {
  node: SessionNode
  activeID: string
  expanded: Set<string>
  forceExpanded: boolean
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  depth?: number
}) {
  const hasChildren = node.children.length > 0
  const isExpanded = forceExpanded || expanded.has(node.session.id)
  return (
    <div className="session-tree">
      <div className="session-tree-row" style={{ '--tree-depth': depth } as React.CSSProperties}>
        {hasChildren ? (
          <button
            aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${node.session.title}`}
            aria-expanded={isExpanded}
            className="session-tree-toggle"
            onClick={() => onToggle(node.session.id)}
          >
            <IconChevronRight size={14} className={cn(isExpanded && 'expanded')} />
          </button>
        ) : <span className="session-tree-spacer">{node.session.isSubsession && <IconGitBranch size={12} />}</span>}
        <SessionRow session={node.session} active={node.session.id === activeID} onClick={() => onSelect(node.session.id)} />
      </div>
      {hasChildren && isExpanded && node.children.map(child => (
        <SessionTree
          key={child.session.id}
          node={child}
          activeID={activeID}
          expanded={expanded}
          forceExpanded={forceExpanded}
          onToggle={onToggle}
          onSelect={onSelect}
          depth={depth + 1}
        />
      ))}
    </div>
  )
}

function SessionRow({ session, active, onClick }: { session: Session; active: boolean; onClick: () => void }) {
  return (
    <button className={cn('session-row', active && 'active')} onClick={onClick}>
      <div className={cn('source-icon', `source-${session.source}`)}>{sourceIcon(session.source, 15)}</div>
      <div className="min-w-0 flex-1">
        <div className="session-row-top"><strong>{session.title || 'Untitled session'}</strong><time>{relativeTime(session.updatedAt || session.createdAt)}</time></div>
        <div className="session-row-meta">
          <span>{session.isSubsession ? session.agent || 'Sub-session' : baseName(session.projectPath) || sourceLabel(session.source)}</span>
          {session.childCount > 0 && <><i /><span>{session.childCount} sub-session{session.childCount === 1 ? '' : 's'}</span></>}
          {session.messageCount > 0 && <><i /> <span>{session.messageCount} messages</span></>}
        </div>
      </div>
    </button>
  )
}

function SessionHeader({ session, parent, onOpenNav, onSelectParent, onDelete }: {
  session: Session
  parent?: Session
  onOpenNav: () => void
  onSelectParent: (id: string) => void
  onDelete: () => void
}) {
  return (
    <header className="session-header">
      <Button aria-label="Open session navigation" variant="ghost" size="icon" className="mobile-session-menu" onClick={onOpenNav}>
        <IconMenu2 size={19} />
      </Button>
      <div className="session-header-info min-w-0 flex-1">
        <div className="header-title-row">
          <h1 title={session.title}>{session.title}</h1>
          {session.isSubsession && <Badge variant="outline" className="subsession-badge">Sub-session</Badge>}
          <Badge variant="outline" className="source-badge">{sourceLabel(session.source)}</Badge>
        </div>
        <div className="header-meta">
          {parent && <><button className="parent-session-link" onClick={() => onSelectParent(parent.id)}><IconGitBranch size={11} />{parent.title}</button><i /></>}
          {session.projectPath && <span title={session.projectPath}>{baseName(session.projectPath)}</span>}
          {session.model && <><i /><span>{session.model}</span></>}
        </div>
      </div>
      <Tooltip label="Delete session">
        <Button aria-label="Delete session" variant="ghost" size="icon" className="text-muted-foreground hover:bg-destructive/10 hover:text-red-300" onClick={onDelete}>
          <IconTrash size={17} />
        </Button>
      </Tooltip>
    </header>
  )
}

function buildSessionForest(sessions: Session[]): SessionForest {
  const nodes = new Map(sessions.map(session => [session.id, { session, children: [] as SessionNode[] }]))
  const roots: SessionNode[] = []
  const detached: SessionNode[] = []
  for (const node of nodes.values()) {
    const parent = node.session.parentId ? nodes.get(node.session.parentId) : undefined
    if (parent && parent !== node) parent.children.push(node)
    else if (node.session.isSubsession) detached.push(node)
    else roots.push(node)
  }
  const sortNodes = (items: SessionNode[]) => {
    items.sort((left, right) => sessionTimestamp(right) - sessionTimestamp(left))
    items.forEach(item => sortNodes(item.children))
  }
  sortNodes(roots)
  sortNodes(detached)
  return { roots, detached }
}

function filterSessionForest(forest: SessionForest, query: string): SessionForest {
  if (!query) return forest
  return {
    roots: filterSessionTree(forest.roots, query),
    detached: filterSessionTree(forest.detached, query),
  }
}

function filterSessionTree(nodes: SessionNode[], query: string): SessionNode[] {
  if (!query) return nodes
  return nodes.flatMap(node => {
    const children = filterSessionTree(node.children, query)
    const matches = [node.session.title, node.session.threadId, node.session.projectPath, node.session.model, node.session.agent]
      .join('\n').toLowerCase().includes(query)
    return matches || children.length ? [{ ...node, children: matches ? node.children : children }] : []
  })
}

function toggleExpanded(setExpanded: React.Dispatch<React.SetStateAction<Set<string>>>) {
  return (id: string) => setExpanded(current => {
    const next = new Set(current)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
}

function sessionTimestamp(node: SessionNode): number {
  const own = new Date(node.session.updatedAt || node.session.createdAt).valueOf() || 0
  return node.children.reduce((latest, child) => Math.max(latest, sessionTimestamp(child)), own)
}

function sessionTreeContains(node: SessionNode, sessionID: string): boolean {
  return node.session.id === sessionID || node.children.some(child => sessionTreeContains(child, sessionID))
}

function Transcript({ detail }: { detail: SessionDetail }) {
  const [search, setSearch] = useState('')
  const [matchIndex, setMatchIndex] = useState(0)
  const [showLatest, setShowLatest] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const term = search.trim().toLocaleLowerCase()
  const matches = useMemo(() => term ? detail.messages.filter(message => message.content.toLocaleLowerCase().includes(term)) : [], [detail.messages, term])
  const activeMatch = matches[matchIndex]?.id
  useEffect(() => {
    const element = scrollRef.current
    if (element) setShowLatest(element.scrollHeight - element.scrollTop - element.clientHeight > 400)
  }, [detail.messages])
  useEffect(() => {
    if (!activeMatch) return
    const element = document.getElementById(activeMatch)
    if (element instanceof HTMLDetailsElement) element.open = true
    element?.scrollIntoView({ block: 'center', behavior: 'instant' })
  }, [activeMatch])
  const totals = useMemo(() => detail.messages.reduce((sum, message) => ({
    input: sum.input + message.tokensInput,
    output: sum.output + message.tokensOutput,
    cached: sum.cached + message.tokensCacheRead,
    written: sum.written + message.tokensCacheWrite,
  }), { input: 0, output: 0, cached: 0, written: 0 }), [detail.messages])
  const usage = detail.usage
  const input = usage?.input_tokens ?? totals.input
  const output = usage?.output_tokens ?? totals.output
  const cached = usage?.cached_input_tokens ?? totals.cached
  const recorded = input + output + cached + totals.written > 0
  const toolsByMessage = useMemo(() => {
    const grouped = new Map<string, ToolCall[]>()
    for (const tool of detail.toolCalls ?? []) grouped.set(tool.messageId, [...(grouped.get(tool.messageId) ?? []), tool])
    return grouped
  }, [detail.toolCalls])
  const orphanTools = (detail.toolCalls ?? []).filter(tool => !tool.messageId || !detail.messages.some(message => message.id === tool.messageId))
  return (
    <div className="conversation">
      <div className="conversation-toolbar">
        <div className="conversation-search">
          <IconSearch size={14} aria-hidden="true" />
          <input aria-label="Find in conversation" placeholder="Find in conversation…" value={search} onChange={event => { setSearch(event.target.value); setMatchIndex(0) }} onKeyDown={event => {
            if (event.key === 'Escape') setSearch('')
            if (event.key === 'Enter' && matches.length) setMatchIndex(index => (index + (event.shiftKey ? matches.length - 1 : 1)) % matches.length)
          }} />
          {search && <><span aria-live="polite">{matches.length ? `${matchIndex + 1}/${matches.length}` : 'No matches'}</span><button aria-label="Next match" disabled={!matches.length} onClick={() => setMatchIndex(index => (index + 1) % matches.length)}><IconChevronDown size={16} /></button><button aria-label="Clear search" onClick={() => setSearch('')}><IconX size={14} /></button></>}
        </div>
        <div className="conversation-totals" aria-label="Recorded session token usage">
          {recorded ? <><span>Input <b>{formatNumber(input)}</b></span><span>Output <b>{formatNumber(output)}</b></span>{cached > 0 && <span>Cache read <b>{formatNumber(cached)}</b></span>}{totals.written > 0 && <span>Cache write <b>{formatNumber(totals.written)}</b></span>}{usage && usage.reasoning_output_tokens > 0 && <span>Reasoning <b>{formatNumber(usage.reasoning_output_tokens)}</b></span>}</> : <span>Token usage not recorded</span>}
        </div>
      </div>
    <div className="transcript" id="transcript" ref={scrollRef} onScroll={event => {
      const element = event.currentTarget
      setShowLatest(element.scrollHeight - element.scrollTop - element.clientHeight > 400)
    }}>
      <div className="transcript-inner">
        {(detail.warnings ?? []).length > 0 && <div className="warning-card"><IconAlertCircle size={16} /> Some records could not be fully parsed.</div>}
        {detail.messages.map(message => <div key={`${message.sequence}-${message.id}`} className={cn(activeMatch === message.id && 'search-match')}><MessageCard message={message} source={detail.session.source} tools={toolsByMessage.get(message.id) ?? []} /></div>)}
        {orphanTools.map(tool => <ToolCard key={tool.id} tool={tool} />)}
        {!detail.messages.length && !orphanTools.length && <div className="transcript-empty">This session has no viewable messages.</div>}
      </div>
    </div>
    {showLatest && <button className="jump-latest" onClick={() => { setSearch(''); scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'instant' }) }}><IconChevronDown size={15} />Latest</button>}
    </div>
  )
}

function MessageCard({ message, source, tools }: { message: Message; source: SourceName; tools: ToolCall[] }) {
  const [copied, setCopied] = useState(false)
  const isReasoning = message.role === 'reasoning'
  if (isReasoning) {
    return (
      <details id={message.id} className="reasoning-card">
        <summary><IconSparkles size={15} /><span>Reasoning summary</span><IconChevronDown size={15} className="ml-auto chevron" /></summary>
        <div className="reasoning-content"><RichText text={message.content} /></div>
      </details>
    )
  }
  if (message.role === 'system') {
    return (
      <details id={message.id} className="context-card">
        <summary><IconCode size={15} /><span>Session context</span><time>{formatTime(message.createdAt)}</time><IconChevronDown size={15} className="ml-auto chevron" /></summary>
        <div className="context-content"><RichText text={message.content} /></div>
      </details>
    )
  }
  return (
    <article id={message.id} className={cn('message-card', `role-${message.role}`)}>
      <div className="message-body">
        <div className="message-heading">
          <strong>{message.role === 'assistant' ? sourceLabel(source) : roleLabel(message.role)}</strong>
          {message.createdAt && <time>{formatTime(message.createdAt)}</time>}
          <Tooltip label={copied ? 'Copied' : 'Copy message'}>
            <Button aria-label="Copy message" variant="ghost" size="icon" className="ml-auto size-7 text-muted-foreground" onClick={async () => {
              await navigator.clipboard.writeText(message.content)
              setCopied(true)
              setTimeout(() => setCopied(false), 1000)
            }}>{copied ? <IconCheck size={14} /> : <IconCopy size={14} />}</Button>
          </Tooltip>
        </div>
        <div className="message-content"><RichText text={message.content} /></div>
        <MessageUsage message={message} />
        {tools.map(tool => <ToolCard key={tool.id} tool={tool} />)}
      </div>
    </article>
  )
}

function ToolCard({ tool }: { tool: ToolCall }) {
  return (
    <details className="tool-card">
      <summary>
        <div className="tool-icon">{tool.kind === 'shell' ? <IconTerminal2 size={14} /> : <IconCode size={14} />}</div>
        <strong>{tool.name || 'Tool'}</strong>
        <span>{tool.status || 'unknown'}</span>
        <IconChevronDown size={14} className="ml-auto chevron" />
      </summary>
      <div className="tool-content">
        {tool.input && <ToolSection label="Input" value={tool.input} />}
        {tool.output && <ToolSection label="Output" value={tool.output} />}
      </div>
    </details>
  )
}

function ToolSection({ label, value }: { label: string; value: string }) {
  return <section><div>{label}</div><pre><code>{value}</code></pre></section>
}

function RichText({ text }: { text: string }) {
  const chunks = text.split(/```([^\n`]*)\n([\s\S]*?)```/g)
  return <>{chunks.map((chunk, index) => index % 3 === 2 ? <pre key={index}><code>{chunk.replace(/\n$/, '')}</code></pre> : index % 3 === 1 ? null : <p key={index}>{chunk}</p>)}</>
}

function MessageUsage({ message }: { message: Message }) {
  if (message.role !== 'assistant') return null
  const fields = [['Input', message.tokensInput], ['Output', message.tokensOutput], ['Reasoning', message.tokensReasoning], ['Cache read', message.tokensCacheRead], ['Cache write', message.tokensCacheWrite]] as const
  return <div className="message-usage">{message.model && <span className="usage-model">{message.model}</span>}{fields.filter(([, count]) => count > 0).map(([label, count]) => <span key={label}>{label} <b>{formatNumber(count)}</b></span>)}{message.cost > 0 && <span>${message.cost.toFixed(4)}</span>}</div>
}

function SessionListSkeleton() {
  return <div className="space-y-2 p-2">{Array.from({ length: 7 }).map((_, index) => <div className="flex gap-3 p-2" key={index}><Skeleton className="size-8" /><div className="flex-1 space-y-2"><Skeleton className="h-3 w-4/5" /><Skeleton className="h-2.5 w-2/5" /></div></div>)}</div>
}

function DetailSkeleton() {
  return <div className="p-6"><div className="mb-10 flex gap-3"><Skeleton className="size-9" /><div className="space-y-2"><Skeleton className="h-5 w-64" /><Skeleton className="h-3 w-96" /></div></div><div className="mx-auto max-w-3xl space-y-6">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className={cn('h-28', index % 2 && 'ml-20')} />)}</div></div>
}

function sourceIcon(source: SourceName, size: number) {
  if (source === 'codex') return <IconBrandOpenai size={size} />
  if (source === 'claude') return <IconSparkles size={size} />
  return <IconCode size={size} />
}
function sourceLabel(source: string) { return source === 'opencode' ? 'OpenCode' : source === 'codex' ? 'Codex' : source === 'claude' ? 'Claude' : source }
function roleLabel(role: string) { return role === 'user' ? 'You' : role === 'assistant' ? 'Assistant' : role === 'system' ? 'Context' : role === 'tool' ? 'Tool' : role || 'Message' }
function baseName(path: string) { return path.split(/[\\/]/).filter(Boolean).pop() ?? '' }
function firstLine(value: string) { return value.trim().split('\n').find(Boolean)?.slice(0, 90) || 'Prompt' }
function formatNumber(value: number) { return new Intl.NumberFormat().format(value) }
function formatDate(value: string) { if (!value) return 'Unknown date'; const date = new Date(value); return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date) }
function formatTime(value: string) { if (!value) return ''; const date = new Date(value); return Number.isNaN(date.valueOf()) ? '' : new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(date) }
function relativeTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return ''
  const delta = date.valueOf() - Date.now()
  const minutes = Math.round(delta / 60000)
  if (Math.abs(minutes) < 60) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(hours, 'hour')
  const days = Math.round(hours / 24)
  if (Math.abs(days) < 30) return new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' }).format(days, 'day')
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(date)
}

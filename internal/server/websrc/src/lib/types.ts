export type SourceName = 'opencode' | 'codex' | 'claude'

export interface Session {
  id: string
  nativeId: string
  threadId: string
  parentId?: string
  parentThreadId?: string
  isSubsession: boolean
  childCount: number
  source: SourceName
  title: string
  projectPath: string
  createdAt: string
  updatedAt: string
  provider: string
  model: string
  agent: string
  origin: string
  messageCount: number
}

export interface Message {
  id: string
  sequence: number
  role: string
  content: string
  createdAt: string
  provider: string
  model: string
  finish: string
  cost: number
  tokensInput: number
  tokensOutput: number
  tokensReasoning: number
  tokensCacheRead: number
  tokensCacheWrite: number
}

export interface ToolCall {
  id: string
  messageId: string
  sequence: number
  name: string
  kind: string
  status: string
  input: string
  output: string
}

export interface SessionDetail {
  session: Session
  messages: Message[]
  toolCalls: ToolCall[]
  warnings: string[]
}

export interface SourceStatus {
  name: SourceName
  available: boolean
  count: number
  error?: string
}

export interface Catalog {
  sessions: Session[]
  sources: SourceStatus[]
}

import { IconBrandOpenai, IconCode, IconSparkles } from '@tabler/icons-react'
import type { SourceName } from './types'

export function sourceIcon(source: SourceName, size: number) {
  if (source === 'codex') return <IconBrandOpenai size={size} />
  if (source === 'claude') return <IconSparkles size={size} />
  return <IconCode size={size} />
}

export function sourceLabel(source: string) {
  return source === 'opencode' ? 'OpenCode' : source === 'codex' ? 'Codex' : source === 'claude' ? 'Claude' : source
}

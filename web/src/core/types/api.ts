// Hand-written types for the Go DTOs in internal/webui/dto.go,
// internal/webui/handlers_bot_settings.go and internal/builtin/settings.go.
// Keep them in sync with those files.

export interface ChatSummary {
  id: number
  title: string
  last_seen_at?: string
}

export interface SessionResponse {
  authenticated: boolean
  username?: string
}

export interface RuleInfo {
  id: number
  pattern: string
  enabled: boolean
  updated_at: string
}

export interface BuiltinRuleInfo {
  id: string
  name: string
  category: string
  description: string
  conditions: string[]
  pattern?: string
  enabled: boolean
  effective: boolean
}

export interface BuiltinStatus {
  enabled: boolean
  library_version: string
  rules: BuiltinRuleInfo[]
}

export interface BuiltinHit {
  id: string
  name: string
  category: string
  evidence: string[]
}

export interface BotSettings {
  bio_check_enabled: boolean
  cross_group_management: boolean
  owner_user_ids: number[]
}

export interface ApiError extends Error {
  status?: number
  code?: string
}

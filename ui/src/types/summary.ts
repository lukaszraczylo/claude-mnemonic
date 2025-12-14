export interface SessionSummary {
  id: number
  sdk_session_id: string
  project: string
  request: string
  investigated: string
  learned: string
  completed: string
  next_steps: string
  notes: string
  prompt_number: number
  discovery_tokens: number
  created_at: string
  created_at_epoch: number
}

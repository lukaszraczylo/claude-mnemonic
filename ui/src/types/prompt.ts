export interface UserPrompt {
  id: number
  claude_session_id: string
  sdk_session_id: string
  project: string
  prompt_number: number
  prompt_text: string
  matched_observations: number
  created_at: string
  created_at_epoch: number
}

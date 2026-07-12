variable "project_id" {
  type        = string
  description = "Google Cloud project ID."
}

variable "region" {
  type        = string
  description = "Cloud Run region."
  default     = "us-central1"
}

variable "vertex_location" {
  type        = string
  description = "Vertex AI endpoint location."
  default     = "global"
}

variable "service_name" {
  type    = string
  default = "discord-daily-log"
}

variable "image" {
  type        = string
  description = "Immutable Artifact Registry image reference."
}

variable "app_service_account_email" {
  type = string
}

variable "task_service_account_email" {
  type = string
}

variable "task_queue" {
  type    = string
  default = "discord-asks"
}

variable "gemini_model" {
  type    = string
  default = "gemini-2.5-flash-lite"
}

variable "discord_application_id" {
  type = string
}

variable "discord_public_key" {
  type      = string
  sensitive = true
}

variable "discord_allowed_user_id" {
  type = string
}

variable "discord_allowed_guild_id" {
  type = string
}

variable "discord_allowed_channel_ids" {
  type = list(string)
}

variable "goal_seed" {
  type        = string
  description = "Natural-language goal used until /goal is called."
  default     = "Track calories, protein, carbohydrates, fat, fiber, all available vitamins and minerals, and dietary variety/antioxidant-rich foods."
}

variable "discord_bot_token_secret" {
  type    = string
  default = "discord-bot-token"
}

variable "usda_api_key_secret" {
  type    = string
  default = "usda-api-key"
}

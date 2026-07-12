variable "project_id" {
  description = "Google Cloud project ID."
  type        = string
}

variable "region" {
  description = "Free-tier-friendly region used for Cloud Run, Firestore, Tasks, and Artifact Registry."
  type        = string
  default     = "us-central1"
}

variable "artifact_repository" {
  description = "Artifact Registry repository name."
  type        = string
  default     = "discord-daily-log"
}

variable "task_queue" {
  description = "Cloud Tasks queue name."
  type        = string
  default     = "discord-asks"
}

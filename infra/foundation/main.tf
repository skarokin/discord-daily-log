terraform {
  required_version = ">= 1.8.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0, < 8.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_project" "current" {
  project_id = var.project_id
}

locals {
  services = toset([
    "aiplatform.googleapis.com",
    "artifactregistry.googleapis.com",
    "cloudbuild.googleapis.com",
    "cloudtasks.googleapis.com",
    "firestore.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
  ])
}

resource "google_project_service" "required" {
  for_each = local.services

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "app" {
  location      = var.region
  repository_id = var.artifact_repository
  description   = "Discord daily nutrition bot images"
  format        = "DOCKER"

  depends_on = [google_project_service.required]
}

resource "google_firestore_database" "default" {
  project                     = var.project_id
  name                        = "(default)"
  location_id                 = var.region
  type                        = "FIRESTORE_NATIVE"
  delete_protection_state     = "DELETE_PROTECTION_ENABLED"
  deletion_policy             = "ABANDON"
  concurrency_mode            = "OPTIMISTIC"
  app_engine_integration_mode = "DISABLED"

  depends_on = [google_project_service.required]
}

resource "google_secret_manager_secret" "discord_bot_token" {
  secret_id = "discord-bot-token"
  replication {
    auto {}
  }
  depends_on = [google_project_service.required]
}

resource "google_secret_manager_secret" "usda_api_key" {
  secret_id = "usda-api-key"
  replication {
    auto {}
  }
  depends_on = [google_project_service.required]
}

# Secret values are deliberately absent. Add versions manually with gcloud.

resource "google_service_account" "app" {
  account_id   = "discord-daily-log"
  display_name = "Discord daily nutrition bot"
}

resource "google_service_account" "task_invoker" {
  account_id   = "discord-task-invoker"
  display_name = "Cloud Tasks OIDC identity"
}

resource "google_cloud_tasks_queue" "ask" {
  name     = var.task_queue
  location = var.region

  rate_limits {
    max_concurrent_dispatches = 1
    max_dispatches_per_second = 1
  }

  retry_config {
    max_attempts       = 3
    max_retry_duration = "900s"
    min_backoff        = "5s"
    max_backoff        = "60s"
  }

  depends_on = [google_project_service.required]
}

locals {
  app_roles = toset([
    "roles/aiplatform.user",
    "roles/cloudtasks.enqueuer",
    "roles/datastore.user",
    "roles/logging.logWriter",
  ])
}

resource "google_project_iam_member" "app" {
  for_each = local.app_roles
  project  = var.project_id
  role     = each.value
  member   = "serviceAccount:${google_service_account.app.email}"
}

resource "google_secret_manager_secret_iam_member" "discord_bot_token" {
  secret_id = google_secret_manager_secret.discord_bot_token.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.app.email}"
}

resource "google_secret_manager_secret_iam_member" "usda_api_key" {
  secret_id = google_secret_manager_secret.usda_api_key.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.app.email}"
}

resource "google_service_account_iam_member" "app_can_use_task_identity" {
  service_account_id = google_service_account.task_invoker.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.app.email}"
}

resource "google_service_account_iam_member" "tasks_can_mint_oidc" {
  service_account_id = google_service_account.task_invoker.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:service-${data.google_project.current.number}@gcp-sa-cloudtasks.iam.gserviceaccount.com"

  depends_on = [google_project_service.required]
}

resource "google_artifact_registry_repository_iam_member" "cloud_build_writer" {
  project    = var.project_id
  location   = google_artifact_registry_repository.app.location
  repository = google_artifact_registry_repository.app.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${data.google_project.current.number}@cloudbuild.gserviceaccount.com"
}

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

resource "google_cloud_run_v2_service" "app" {
  name                = var.service_name
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"

  template {
    service_account                  = var.app_service_account_email
    timeout                          = "900s"
    max_instance_request_concurrency = 4

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      image = var.image

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      ports {
        container_port = 8080
      }

      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }
      env {
        name  = "GOOGLE_CLOUD_LOCATION"
        value = var.vertex_location
      }
      env {
        name  = "GEMINI_MODEL"
        value = var.gemini_model
      }
      env {
        name  = "DISCORD_APPLICATION_ID"
        value = var.discord_application_id
      }
      env {
        name  = "DISCORD_PUBLIC_KEY"
        value = var.discord_public_key
      }
      env {
        name  = "DISCORD_ALLOWED_USER_ID"
        value = var.discord_allowed_user_id
      }
      env {
        name  = "DISCORD_ALLOWED_GUILD_ID"
        value = var.discord_allowed_guild_id
      }
      env {
        name  = "DISCORD_ALLOWED_CHANNEL_IDS"
        value = join(",", var.discord_allowed_channel_ids)
      }
      env {
        name  = "CLOUD_TASKS_LOCATION"
        value = var.region
      }
      env {
        name  = "CLOUD_TASKS_QUEUE"
        value = var.task_queue
      }
      env {
        name  = "TASK_SERVICE_ACCOUNT_EMAIL"
        value = var.task_service_account_email
      }
      env {
        name  = "GOAL_SEED"
        value = var.goal_seed
      }
      env {
        name  = "DEV_MODE"
        value = "false"
      }
      env {
        name = "DISCORD_BOT_TOKEN"
        value_source {
          secret_key_ref {
            secret  = var.discord_bot_token_secret
            version = "latest"
          }
        }
      }
      env {
        name = "USDA_API_KEY"
        value_source {
          secret_key_ref {
            secret  = var.usda_api_key_secret
            version = "latest"
          }
        }
      }

      startup_probe {
        initial_delay_seconds = 0
        timeout_seconds       = 3
        period_seconds        = 5
        failure_threshold     = 12
        http_get {
          path = "/healthz"
          port = 8080
        }
      }

      liveness_probe {
        timeout_seconds   = 3
        period_seconds    = 30
        failure_threshold = 3
        http_get {
          path = "/healthz"
          port = 8080
        }
      }
    }
  }
}

resource "google_cloud_run_v2_service_iam_member" "public" {
  project  = var.project_id
  location = google_cloud_run_v2_service.app.location
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

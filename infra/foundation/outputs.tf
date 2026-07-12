output "artifact_repository" {
  value = google_artifact_registry_repository.app.repository_id
}

output "app_service_account_email" {
  value = google_service_account.app.email
}

output "task_service_account_email" {
  value = google_service_account.task_invoker.email
}

output "task_queue" {
  value = google_cloud_tasks_queue.ask.name
}

output "secret_names" {
  value = {
    discord_bot_token = google_secret_manager_secret.discord_bot_token.secret_id
    usda_api_key      = google_secret_manager_secret.usda_api_key.secret_id
  }
}

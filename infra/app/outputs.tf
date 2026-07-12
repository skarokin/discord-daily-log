output "service_url" {
  value = google_cloud_run_v2_service.app.uri
}

output "interactions_endpoint" {
  value = "${google_cloud_run_v2_service.app.uri}/interactions"
}

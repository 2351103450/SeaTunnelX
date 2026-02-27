package config

import "testing"

func TestValidateConfig_ObservabilityDisabled(t *testing.T) {
	c := &configModel{}
	c.Observability.Enabled = false
	if err := validateConfig(c); err != nil {
		t.Fatalf("expected nil error when observability is disabled, got: %v", err)
	}
}

func TestValidateConfig_MissingExternalURL(t *testing.T) {
	c := &configModel{}
	c.Observability.Enabled = true
	c.Observability.Prometheus.URL = "http://127.0.0.1:9090"
	c.Observability.Alertmanager.URL = "http://127.0.0.1:9093"
	c.Observability.Grafana.URL = "http://127.0.0.1:3000"
	c.Observability.Prometheus.HTTPSDPath = "/api/v1/monitoring/prometheus/discovery"
	c.Observability.Alertmanager.WebhookPath = "/api/v1/monitoring/alertmanager/webhook"

	if err := validateConfig(c); err == nil {
		t.Fatalf("expected validation error when app.external_url is empty")
	}
}

func TestValidateConfig_RemoteObservabilityHappyPath(t *testing.T) {
	c := &configModel{}
	c.App.ExternalURL = "https://seatunnelx.example.com"
	c.Observability.Enabled = true
	c.Observability.Prometheus.URL = "http://127.0.0.1:9090"
	c.Observability.Alertmanager.URL = "http://127.0.0.1:9093"
	c.Observability.Grafana.URL = "http://127.0.0.1:3000"
	c.Observability.Prometheus.HTTPSDPath = "/api/v1/monitoring/prometheus/discovery"
	c.Observability.Alertmanager.WebhookPath = "/api/v1/monitoring/alertmanager/webhook"

	if err := validateConfig(c); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

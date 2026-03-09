/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/seatunnel/seatunnelX/internal/config"
)

// GetAlertPolicyCenterBootstrap returns unified policy-center bootstrap payload.
// GetAlertPolicyCenterBootstrap 返回统一策略中心初始化数据。
func (s *Service) GetAlertPolicyCenterBootstrap(ctx context.Context) (*AlertPolicyCenterBootstrapData, error) {
	integrationStatus, err := s.GetIntegrationStatus(ctx)
	if err != nil {
		return nil, err
	}

	componentMap := indexIntegrationComponents(integrationStatus.Components)
	metricsStatus, metricsReason := resolveMetricsPolicyCapability(componentMap)
	customPromQLStatus, customPromQLReason := resolveCustomPromQLCapability(componentMap)
	remoteIngestStatus, remoteIngestReason := resolveRemoteIngestCapability(componentMap)

	capabilities := []*AlertPolicyCapabilityDTO{
		{
			Key:     AlertPolicyCapabilityKeyPlatformHealth,
			Title:   "Platform Health Policies",
			Summary: "Use SeaTunnelX-managed runtime and cluster signals to detect health issues even without Prometheus.",
			Status:  AlertPolicyCapabilityStatusAvailable,
		},
		{
			Key:       AlertPolicyCapabilityKeyMetricsTemplates,
			Title:     "Metrics Templates",
			Summary:   "Enable CPU, memory, FD, failed-job, and other metrics-driven alert policies through Prometheus.",
			Status:    metricsStatus,
			Reason:    metricsReason,
			DependsOn: []string{"prometheus", "seatunnel_metrics"},
		},
		{
			Key:       AlertPolicyCapabilityKeyCustomPromQL,
			Title:     "Custom PromQL",
			Summary:   "Create productized metric policies while still allowing advanced teams to write custom PromQL rules.",
			Status:    customPromQLStatus,
			Reason:    customPromQLReason,
			DependsOn: []string{"prometheus", "seatunnel_metrics"},
		},
		{
			Key:       AlertPolicyCapabilityKeyRemoteIngest,
			Title:     "Remote Alert Ingest",
			Summary:   "Ingest Alertmanager webhook alerts into the unified alert center and notification pipeline.",
			Status:    remoteIngestStatus,
			Reason:    remoteIngestReason,
			DependsOn: []string{"alertmanager"},
		},
		{
			Key:     AlertPolicyCapabilityKeyWebhookNotification,
			Title:   "Webhook / IM Notifications",
			Summary: "Deliver alert notifications through webhook-compatible channels such as Webhook, WeCom, DingTalk, and Feishu.",
			Status:  AlertPolicyCapabilityStatusAvailable,
		},
		{
			Key:     AlertPolicyCapabilityKeyInAppNotification,
			Title:   "In-App Notification Center",
			Summary: "Provide a built-in notification inbox, receiver experience, and recovery follow-up inside SeaTunnelX.",
			Status:  AlertPolicyCapabilityStatusPlanned,
			Reason:  "Planned next step after the unified policy domain lands.",
		},
	}

	builders := []*AlertPolicyBuilderDTO{
		{
			Key:           AlertPolicyBuilderKindPlatformHealth,
			Title:         "Platform Health Policies",
			Description:   "Create unified policies for cluster availability, node liveness, process stability, and operation failures.",
			Status:        AlertPolicyCapabilityStatusAvailable,
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			Recommended:   true,
		},
		{
			Key:           AlertPolicyBuilderKindMetricsTemplate,
			Title:         "Metrics Templates",
			Description:   "Start from curated metric templates for CPU, memory, FD, failed jobs, and other Prometheus-backed signals.",
			Status:        metricsStatus,
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   true,
		},
		{
			Key:           AlertPolicyBuilderKindCustomPromQL,
			Title:         "Custom PromQL",
			Description:   "Unlock advanced policies when Prometheus is healthy and metrics targets are reachable.",
			Status:        customPromQLStatus,
			CapabilityKey: AlertPolicyCapabilityKeyCustomPromQL,
			Recommended:   false,
		},
	}

	return &AlertPolicyCenterBootstrapData{
		GeneratedAt:    time.Now().UTC(),
		CapabilityMode: "unified_capability_aware",
		Capabilities:   capabilities,
		Builders:       builders,
		Templates:      defaultAlertPolicyTemplateSummaries(),
		Components:     cloneIntegrationComponents(integrationStatus.Components),
	}, nil
}

func indexIntegrationComponents(components []*IntegrationComponentStatus) map[string]*IntegrationComponentStatus {
	result := make(map[string]*IntegrationComponentStatus, len(components))
	for _, component := range components {
		if component == nil {
			continue
		}
		result[strings.TrimSpace(strings.ToLower(component.Name))] = component
	}
	return result
}

func cloneIntegrationComponents(components []*IntegrationComponentStatus) []*IntegrationComponentStatus {
	if len(components) == 0 {
		return []*IntegrationComponentStatus{}
	}
	result := make([]*IntegrationComponentStatus, 0, len(components))
	for _, component := range components {
		if component == nil {
			continue
		}
		cloned := *component
		result = append(result, &cloned)
	}
	return result
}

func resolveMetricsPolicyCapability(componentMap map[string]*IntegrationComponentStatus) (AlertPolicyCapabilityStatus, string) {
	if !config.Config.Observability.Enabled {
		return AlertPolicyCapabilityStatusUnavailable, "Observability is disabled. Enable the Prometheus stack to unlock metric policies."
	}
	if reason := explainUnhealthyComponent(componentMap["prometheus"], "Prometheus"); reason != "" {
		return AlertPolicyCapabilityStatusUnavailable, reason
	}
	if reason := explainUnhealthyComponent(componentMap["seatunnel_metrics"], "SeaTunnel metrics targets"); reason != "" {
		return AlertPolicyCapabilityStatusUnavailable, reason
	}
	return AlertPolicyCapabilityStatusAvailable, ""
}

func resolveCustomPromQLCapability(componentMap map[string]*IntegrationComponentStatus) (AlertPolicyCapabilityStatus, string) {
	status, reason := resolveMetricsPolicyCapability(componentMap)
	if status != AlertPolicyCapabilityStatusAvailable {
		return status, reason
	}
	return AlertPolicyCapabilityStatusAvailable, ""
}

func resolveRemoteIngestCapability(componentMap map[string]*IntegrationComponentStatus) (AlertPolicyCapabilityStatus, string) {
	if !config.Config.Observability.Enabled {
		return AlertPolicyCapabilityStatusUnavailable, "Observability is disabled. Configure Alertmanager to enable remote alert ingest."
	}
	if reason := explainUnhealthyComponent(componentMap["alertmanager"], "Alertmanager"); reason != "" {
		return AlertPolicyCapabilityStatusUnavailable, reason
	}
	return AlertPolicyCapabilityStatusAvailable, ""
}

func explainUnhealthyComponent(component *IntegrationComponentStatus, displayName string) string {
	if component == nil {
		return fmt.Sprintf("%s status is unavailable.", displayName)
	}
	if component.Healthy {
		return ""
	}
	if strings.TrimSpace(component.Error) != "" {
		return fmt.Sprintf("%s is not ready: %s", displayName, strings.TrimSpace(component.Error))
	}
	if component.StatusCode > 0 {
		return fmt.Sprintf("%s is not ready: HTTP %d.", displayName, component.StatusCode)
	}
	return fmt.Sprintf("%s is not ready.", displayName)
}

func defaultAlertPolicyTemplateSummaries() []*AlertPolicyTemplateSummaryDTO {
	return []*AlertPolicyTemplateSummaryDTO{
		{
			Key:           "master_unavailable",
			Name:          "Master unavailable",
			Description:   "Detect when the managed SeaTunnel cluster has no healthy master / coordinator left.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			Recommended:   true,
		},
		{
			Key:           "worker_insufficient",
			Name:          "Healthy workers below threshold",
			Description:   "Alert when the healthy worker count drops below the configured baseline for one cluster.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			Recommended:   true,
		},
		{
			Key:           AlertRuleKeyNodeOffline,
			Name:          "Node offline",
			Description:   "Detect node heartbeat or runtime visibility loss for a sustained duration.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			LegacyRuleKey: AlertRuleKeyNodeOffline,
			Recommended:   true,
		},
		{
			Key:           "agent_offline",
			Name:          "Agent offline",
			Description:   "Alert when the management plane loses the agent connection required for cluster operations.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			Recommended:   true,
		},
		{
			Key:           AlertRuleKeyProcessCrashed,
			Name:          "Process crashed",
			Description:   "Track repeated process crashes in the managed SeaTunnel runtime.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			LegacyRuleKey: AlertRuleKeyProcessCrashed,
			Recommended:   false,
		},
		{
			Key:           AlertRuleKeyProcessRestartFailed,
			Name:          "Process restart failed",
			Description:   "Alert when automatic restart can no longer recover a failed process.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			LegacyRuleKey: AlertRuleKeyProcessRestartFailed,
			Recommended:   true,
		},
		{
			Key:           AlertRuleKeyProcessRestartLimitReached,
			Name:          "Restart limit reached",
			Description:   "Detect crash-loop style behavior when the managed runtime exceeds restart limits.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			LegacyRuleKey: AlertRuleKeyProcessRestartLimitReached,
			Recommended:   true,
		},
		{
			Key:           AlertRuleKeyClusterRestartRequested,
			Name:          "Cluster restart requested",
			Description:   "Send a notification when a managed cluster restart is triggered from the control plane.",
			Category:      "platform_health",
			SourceKind:    string(AlertPolicyBuilderKindPlatformHealth),
			CapabilityKey: AlertPolicyCapabilityKeyPlatformHealth,
			LegacyRuleKey: AlertRuleKeyClusterRestartRequested,
			Recommended:   false,
		},
		{
			Key:           "cpu_usage_high",
			Name:          "CPU usage high",
			Description:   "Use Prometheus-backed CPU metrics to detect sustained saturation.",
			Category:      "metrics",
			SourceKind:    string(AlertPolicyBuilderKindMetricsTemplate),
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   true,
		},
		{
			Key:           "memory_usage_high",
			Name:          "Memory usage high",
			Description:   "Use Prometheus-backed memory metrics to catch pressure before node instability escalates.",
			Category:      "metrics",
			SourceKind:    string(AlertPolicyBuilderKindMetricsTemplate),
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   true,
		},
		{
			Key:           "fd_usage_high",
			Name:          "FD usage high",
			Description:   "Detect file-descriptor exhaustion risks through the metrics stack.",
			Category:      "metrics",
			SourceKind:    string(AlertPolicyBuilderKindMetricsTemplate),
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   false,
		},
		{
			Key:           "failed_jobs_high",
			Name:          "Failed jobs high",
			Description:   "Alert on elevated failed-job counts or retries using Prometheus-backed application metrics.",
			Category:      "metrics",
			SourceKind:    string(AlertPolicyBuilderKindMetricsTemplate),
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   true,
		},
		{
			Key:           "split_brain_risk",
			Name:          "Split-brain risk",
			Description:   "Reserve a template slot for cluster-consensus or role-divergence metrics once exposed through Prometheus.",
			Category:      "metrics",
			SourceKind:    string(AlertPolicyBuilderKindMetricsTemplate),
			CapabilityKey: AlertPolicyCapabilityKeyMetricsTemplates,
			Recommended:   false,
		},
	}
}

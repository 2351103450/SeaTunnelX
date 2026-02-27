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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BuildPrometheusSDTargets builds Prometheus HTTP SD target groups from managed clusters.
// BuildPrometheusSDTargets 从受管集群构建 Prometheus HTTP SD 目标组。
func (s *Service) BuildPrometheusSDTargets(ctx context.Context) ([]*PrometheusSDTargetGroup, error) {
	targets, err := s.collectManagedMetricsTargets(ctx, true)
	if err != nil {
		return nil, err
	}

	groupMap := make(map[string]*PrometheusSDTargetGroup)
	seen := make(map[string]map[string]struct{})
	groupKeys := make([]string, 0, 8)

	for _, item := range targets {
		if item == nil || !item.Healthy {
			continue
		}

		clusterID := strconv.FormatUint(uint64(item.ClusterID), 10)
		if item.ClusterID == 0 {
			clusterID = "static"
		}
		clusterName := strings.TrimSpace(item.ClusterName)
		if clusterName == "" {
			clusterName = "unknown"
		}
		env := strings.TrimSpace(item.Env)
		if env == "" {
			env = "unknown"
		}

		key := clusterID + "|" + clusterName + "|" + env
		group, ok := groupMap[key]
		if !ok {
			group = &PrometheusSDTargetGroup{
				Targets: make([]string, 0, 2),
				Labels: map[string]string{
					"job":          "seatunnel_engine_http",
					"cluster_id":   clusterID,
					"cluster_name": clusterName,
					"env":          env,
				},
			}
			groupMap[key] = group
			groupKeys = append(groupKeys, key)
			seen[key] = make(map[string]struct{})
		}
		if _, ok := seen[key][item.Target]; ok {
			continue
		}
		seen[key][item.Target] = struct{}{}
		group.Targets = append(group.Targets, item.Target)
	}

	sort.Strings(groupKeys)
	result := make([]*PrometheusSDTargetGroup, 0, len(groupKeys))
	for _, key := range groupKeys {
		group := groupMap[key]
		sort.Strings(group.Targets)
		result = append(result, group)
	}
	return result, nil
}

// HandleAlertmanagerWebhook ingests one Alertmanager webhook payload into persistent records.
// HandleAlertmanagerWebhook 将 Alertmanager webhook 请求写入持久化告警记录。
func (s *Service) HandleAlertmanagerWebhook(ctx context.Context, payload *AlertmanagerWebhookPayload) (*AlertmanagerWebhookResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if payload == nil {
		return nil, fmt.Errorf("empty webhook payload")
	}

	result := &AlertmanagerWebhookResult{
		Received: len(payload.Alerts),
		Stored:   0,
		Errors:   make([]string, 0, 2),
	}
	now := time.Now().UTC()
	for idx, alert := range payload.Alerts {
		if alert == nil {
			continue
		}

		record := normalizeWebhookAlert(payload, alert, now)
		if err := s.repo.UpsertRemoteAlert(ctx, record); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("alert[%d] upsert failed: %v", idx, err))
			continue
		}
		result.Stored++
	}
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func normalizeWebhookAlert(payload *AlertmanagerWebhookPayload, alert *WebhookAlert, now time.Time) *RemoteAlertRecord {
	labels := mergeStringMap(payload.CommonLabels, alert.Labels)
	annotations := mergeStringMap(payload.CommonAnnotations, alert.Annotations)

	startsAt := alert.StartsAt.UTC()
	if startsAt.IsZero() {
		startsAt = now
	}

	status := strings.TrimSpace(alert.Status)
	if status == "" {
		status = strings.TrimSpace(payload.Status)
	}
	if status == "" {
		status = "firing"
	}

	clusterID := strings.TrimSpace(labels["cluster_id"])
	if clusterID == "" {
		clusterID = "unknown"
	}
	clusterName := strings.TrimSpace(labels["cluster_name"])
	if clusterName == "" {
		clusterName = "unknown"
	}
	env := strings.TrimSpace(labels["env"])
	if env == "" {
		env = "unknown"
	}

	fingerprint := strings.TrimSpace(alert.Fingerprint)
	if fingerprint == "" {
		fingerprint = buildFallbackFingerprint(labels, startsAt, alert.GeneratorURL)
	}

	alertName := strings.TrimSpace(labels["alertname"])
	if alertName == "" {
		alertName = "unknown_alert"
	}

	labelsJSON := mustMarshalJSON(labels)
	annotationsJSON := mustMarshalJSON(annotations)

	var endsAtUnix int64
	var resolvedAt *time.Time
	if !alert.EndsAt.IsZero() {
		endsAt := alert.EndsAt.UTC()
		endsAtUnix = endsAt.Unix()
		if strings.EqualFold(status, "resolved") {
			resolvedAt = &endsAt
		}
	}
	if strings.EqualFold(status, "resolved") && resolvedAt == nil {
		t := now
		resolvedAt = &t
	}

	return &RemoteAlertRecord{
		Fingerprint:     fingerprint,
		StartsAt:        startsAt.Unix(),
		Status:          status,
		Receiver:        strings.TrimSpace(payload.Receiver),
		AlertName:       alertName,
		Severity:        strings.TrimSpace(labels["severity"]),
		ClusterID:       clusterID,
		ClusterName:     clusterName,
		Env:             env,
		GeneratorURL:    strings.TrimSpace(alert.GeneratorURL),
		Summary:         strings.TrimSpace(firstNonEmpty(annotations["summary"], annotations["message"])),
		Description:     strings.TrimSpace(firstNonEmpty(annotations["description"], annotations["details"])),
		LabelsJSON:      labelsJSON,
		AnnotationsJSON: annotationsJSON,
		EndsAt:          endsAtUnix,
		ResolvedAt:      resolvedAt,
		LastReceivedAt:  now,
	}
}

func mergeStringMap(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func mustMarshalJSON(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func buildFallbackFingerprint(labels map[string]string, startsAt time.Time, generatorURL string) string {
	raw := mustMarshalJSON(labels) + "|" + startsAt.UTC().Format(time.RFC3339Nano) + "|" + strings.TrimSpace(generatorURL)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

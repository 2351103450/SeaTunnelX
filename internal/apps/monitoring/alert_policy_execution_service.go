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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/seatunnel/seatunnelX/internal/apps/monitor"
)

// AlertPolicyExecutionStateUpdate describes aggregate execution changes for one policy.
// AlertPolicyExecutionStateUpdate 描述单条策略的一次执行聚合更新。
type AlertPolicyExecutionStateUpdate struct {
	MatchCountDelta     int
	DeliveryCountDelta  int
	LastMatchedAt       *time.Time
	LastDeliveredAt     *time.Time
	LastExecutionStatus AlertPolicyExecutionStatus
	LastExecutionError  string
}

type alertPolicyChannelDispatchResult struct {
	DeliveryCountDelta int
	LastDeliveredAt    *time.Time
	Successful         bool
	LastError          string
}

type alertPolicyDispatchSummary struct {
	AttemptedChannels   int
	SuccessfulChannels  int
	FailedChannels      int
	DeliveryCountDelta  int
	LastDeliveredAt     *time.Time
	LastExecutionStatus AlertPolicyExecutionStatus
	LastExecutionError  string
}

// DispatchAlertPolicyEvent evaluates one process event against unified alert policies.
// DispatchAlertPolicyEvent 根据统一告警策略执行一次进程事件评估。
func (s *Service) DispatchAlertPolicyEvent(ctx context.Context, event *monitor.ProcessEvent) error {
	if s.repo == nil || event == nil {
		return nil
	}
	if event.EventType == monitor.EventTypeNodeRecovered {
		return s.dispatchLocalResolvedAlertPolicyEvent(ctx, event)
	}
	if !isAlertableEventType(event.EventType) {
		return nil
	}

	ruleKey := eventTypeToRuleKey(event.EventType)
	if strings.TrimSpace(ruleKey) == "" {
		return nil
	}

	clusterID := strconv.FormatUint(uint64(event.ClusterID), 10)
	policies, err := s.repo.ListEnabledAlertPoliciesByClusterAndLegacyRuleKey(ctx, clusterID, ruleKey)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}

	clusterName := ""
	if s.clusterService != nil && event.ClusterID > 0 {
		clusterInfo, err := s.clusterService.Get(ctx, event.ClusterID)
		if err == nil && clusterInfo != nil {
			clusterName = strings.TrimSpace(clusterInfo.Name)
		}
	}

	channels, err := s.repo.ListNotificationChannels(ctx)
	if err != nil {
		return err
	}
	channelMap := make(map[uint]*NotificationChannel, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		channelMap[channel.ID] = channel
	}

	matchedAt := localAlertPolicyMatchedAt(event)
	var dispatchErrs []error
	for _, policy := range policies {
		if policy == nil {
			continue
		}

		summary, err := s.dispatchLocalAlertPolicy(ctx, event, clusterName, policy, channelMap)
		if err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}

		stateUpdate := &AlertPolicyExecutionStateUpdate{
			MatchCountDelta:     1,
			DeliveryCountDelta:  summary.DeliveryCountDelta,
			LastMatchedAt:       &matchedAt,
			LastDeliveredAt:     summary.LastDeliveredAt,
			LastExecutionStatus: summary.LastExecutionStatus,
			LastExecutionError:  strings.TrimSpace(summary.LastExecutionError),
		}
		if err := s.repo.ApplyAlertPolicyExecutionState(ctx, policy.ID, stateUpdate); err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	return errors.Join(dispatchErrs...)
}

func (s *Service) dispatchLocalResolvedAlertPolicyEvent(ctx context.Context, recoveryEvent *monitor.ProcessEvent) error {
	if s.repo == nil || recoveryEvent == nil || recoveryEvent.EventType != monitor.EventTypeNodeRecovered {
		return nil
	}

	sourceEvent, err := s.resolveLocalRecoveredAlertSourceEvent(ctx, recoveryEvent)
	if err != nil {
		return err
	}
	if sourceEvent == nil {
		return nil
	}

	clusterID := strconv.FormatUint(uint64(sourceEvent.ClusterID), 10)
	policies, err := s.repo.ListEnabledAlertPoliciesByClusterAndLegacyRuleKey(ctx, clusterID, AlertRuleKeyNodeOffline)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}

	clusterName := ""
	if s.clusterService != nil && sourceEvent.ClusterID > 0 {
		clusterInfo, err := s.clusterService.Get(ctx, sourceEvent.ClusterID)
		if err == nil && clusterInfo != nil {
			clusterName = strings.TrimSpace(clusterInfo.Name)
		}
	}

	channels, err := s.repo.ListNotificationChannels(ctx)
	if err != nil {
		return err
	}
	channelMap := make(map[uint]*NotificationChannel, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		channelMap[channel.ID] = channel
	}

	var dispatchErrs []error
	for _, policy := range policies {
		if policy == nil || !policy.SendRecovery {
			continue
		}

		summary, err := s.dispatchLocalResolvedAlertPolicy(ctx, sourceEvent, recoveryEvent, clusterName, policy, channelMap)
		if err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}

		stateUpdate := &AlertPolicyExecutionStateUpdate{
			DeliveryCountDelta:  summary.DeliveryCountDelta,
			LastDeliveredAt:     summary.LastDeliveredAt,
			LastExecutionStatus: summary.LastExecutionStatus,
			LastExecutionError:  strings.TrimSpace(summary.LastExecutionError),
		}
		if err := s.repo.ApplyAlertPolicyExecutionState(ctx, policy.ID, stateUpdate); err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	return errors.Join(dispatchErrs...)
}

func (s *Service) resolveLocalRecoveredAlertSourceEvent(ctx context.Context, recoveryEvent *monitor.ProcessEvent) (*monitor.ProcessEvent, error) {
	if recoveryEvent == nil || s.monitorService == nil {
		return nil, nil
	}

	if offlineEventID := parseLocalRecoveredOfflineEventID(recoveryEvent.Details); offlineEventID > 0 {
		sourceEvent, err := s.monitorService.GetEvent(ctx, offlineEventID)
		if err != nil && !errors.Is(err, monitor.ErrEventNotFound) {
			return nil, err
		}
		if err == nil && sourceEvent != nil && sourceEvent.EventType == monitor.EventTypeNodeOffline {
			return sourceEvent, nil
		}
	}

	sourceEvent, err := s.monitorService.GetLatestNodeEventByTypes(ctx, recoveryEvent.NodeID, []monitor.ProcessEventType{
		monitor.EventTypeNodeOffline,
	})
	if err != nil {
		return nil, err
	}
	if sourceEvent == nil || !sourceEvent.CreatedAt.Before(recoveryEvent.CreatedAt) {
		return nil, nil
	}
	return sourceEvent, nil
}

func (s *Service) dispatchLocalAlertPolicy(
	ctx context.Context,
	event *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	channelMap map[uint]*NotificationChannel,
) (alertPolicyDispatchSummary, error) {
	summary := alertPolicyDispatchSummary{
		LastExecutionStatus: AlertPolicyExecutionStatusMatched,
	}
	if event == nil || policy == nil {
		return summary, nil
	}

	channelIDs := unmarshalAlertPolicyChannelIDs(policy.NotificationChannelIDsJSON)
	if len(channelIDs) == 0 {
		return summary, nil
	}

	var dispatchErrs []error
	for _, channelID := range channelIDs {
		channel := channelMap[channelID]
		if channel == nil || !channel.Enabled {
			continue
		}

		summary.AttemptedChannels++
		result, err := s.dispatchLocalAlertPolicyDelivery(
			ctx,
			event,
			nil,
			clusterName,
			policy,
			channel,
			NotificationDeliveryEventTypeFiring,
			buildLocalAlertSourceKey(event.ID),
			false,
		)
		if result != nil {
			if result.Successful {
				summary.SuccessfulChannels++
			}
			if result.DeliveryCountDelta > 0 {
				summary.DeliveryCountDelta += result.DeliveryCountDelta
			}
			summary.LastDeliveredAt = laterUTCTimePointer(summary.LastDeliveredAt, result.LastDeliveredAt)
			if strings.TrimSpace(result.LastError) != "" {
				summary.LastExecutionError = strings.TrimSpace(result.LastError)
			}
		}
		if err != nil {
			summary.FailedChannels++
			summary.LastExecutionError = strings.TrimSpace(err.Error())
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	summary.LastExecutionStatus = summarizeAlertPolicyExecutionStatus(summary)
	if summary.LastExecutionStatus == AlertPolicyExecutionStatusSent {
		summary.LastExecutionError = ""
	}
	if summary.LastExecutionStatus == AlertPolicyExecutionStatusMatched && summary.SuccessfulChannels == 0 && summary.FailedChannels == 0 {
		summary.LastExecutionError = ""
	}
	return summary, errors.Join(dispatchErrs...)
}

func (s *Service) dispatchLocalResolvedAlertPolicy(
	ctx context.Context,
	sourceEvent *monitor.ProcessEvent,
	recoveryEvent *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	channelMap map[uint]*NotificationChannel,
) (alertPolicyDispatchSummary, error) {
	summary := alertPolicyDispatchSummary{
		LastExecutionStatus: AlertPolicyExecutionStatusMatched,
	}
	if sourceEvent == nil || recoveryEvent == nil || policy == nil {
		return summary, nil
	}

	sourceKey := buildLocalAlertSourceKey(sourceEvent.ID)
	channelIDs := unmarshalAlertPolicyChannelIDs(policy.NotificationChannelIDsJSON)
	if len(channelIDs) == 0 {
		return summary, nil
	}

	var dispatchErrs []error
	for _, channelID := range channelIDs {
		channel := channelMap[channelID]
		if channel == nil || !channel.Enabled {
			continue
		}

		summary.AttemptedChannels++
		result, err := s.dispatchLocalAlertPolicyDelivery(
			ctx,
			sourceEvent,
			recoveryEvent,
			clusterName,
			policy,
			channel,
			NotificationDeliveryEventTypeResolved,
			sourceKey,
			true,
		)
		if result != nil {
			if result.Successful {
				summary.SuccessfulChannels++
			}
			if result.DeliveryCountDelta > 0 {
				summary.DeliveryCountDelta += result.DeliveryCountDelta
			}
			summary.LastDeliveredAt = laterUTCTimePointer(summary.LastDeliveredAt, result.LastDeliveredAt)
			if strings.TrimSpace(result.LastError) != "" {
				summary.LastExecutionError = strings.TrimSpace(result.LastError)
			}
		}
		if err != nil {
			summary.FailedChannels++
			summary.LastExecutionError = strings.TrimSpace(err.Error())
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	summary.LastExecutionStatus = summarizeAlertPolicyExecutionStatus(summary)
	if summary.LastExecutionStatus == AlertPolicyExecutionStatusSent {
		summary.LastExecutionError = ""
	}
	if summary.LastExecutionStatus == AlertPolicyExecutionStatusMatched && summary.SuccessfulChannels == 0 && summary.FailedChannels == 0 {
		summary.LastExecutionError = ""
	}
	return summary, errors.Join(dispatchErrs...)
}

func summarizeAlertPolicyExecutionStatus(summary alertPolicyDispatchSummary) AlertPolicyExecutionStatus {
	switch {
	case summary.SuccessfulChannels > 0 && summary.FailedChannels > 0:
		return AlertPolicyExecutionStatusPartial
	case summary.SuccessfulChannels > 0:
		return AlertPolicyExecutionStatusSent
	case summary.FailedChannels > 0:
		return AlertPolicyExecutionStatusFailed
	default:
		return AlertPolicyExecutionStatusMatched
	}
}

func localAlertPolicyMatchedAt(event *monitor.ProcessEvent) time.Time {
	if event == nil || event.CreatedAt.IsZero() {
		return timeNowUTC()
	}
	return event.CreatedAt.UTC()
}

func laterUTCTimePointer(current *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	next := candidate.UTC()
	if current == nil || current.Before(next) {
		return &next
	}
	return current
}

func (s *Service) dispatchLocalAlertPolicyDelivery(
	ctx context.Context,
	sourceEvent *monitor.ProcessEvent,
	recoveryEvent *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	channel *NotificationChannel,
	deliveryEventType NotificationDeliveryEventType,
	sourceKey string,
	requireFiringSent bool,
) (*alertPolicyChannelDispatchResult, error) {
	if sourceEvent == nil || policy == nil || channel == nil {
		return nil, nil
	}

	sourceKey = strings.TrimSpace(firstNonEmpty(sourceKey, buildLocalAlertSourceKey(sourceEvent.ID)))
	if requireFiringSent {
		firingDelivery, err := s.repo.GetNotificationDeliveryByDedupKey(ctx, sourceKey, channel.ID, string(NotificationDeliveryEventTypeFiring))
		if err != nil {
			return nil, err
		}
		if firingDelivery == nil || NotificationDeliveryStatus(strings.TrimSpace(firingDelivery.Status)) != NotificationDeliveryStatusSent {
			return nil, nil
		}
	}

	delivery, err := s.repo.GetNotificationDeliveryByDedupKey(ctx, sourceKey, channel.ID, string(deliveryEventType))
	if err != nil {
		return nil, err
	}

	if delivery == nil {
		delivery = &NotificationDelivery{
			AlertID:      sourceKey,
			SourceType:   string(AlertSourceTypeLocalProcessEvent),
			SourceKey:    sourceKey,
			PolicyID:     policy.ID,
			ClusterID:    strconv.FormatUint(uint64(sourceEvent.ClusterID), 10),
			ClusterName:  strings.TrimSpace(clusterName),
			AlertName:    strings.TrimSpace(firstNonEmpty(policy.Name, policy.TemplateKey, policy.LegacyRuleKey, "local alert policy")),
			ChannelID:    channel.ID,
			ChannelName:  strings.TrimSpace(channel.Name),
			EventType:    string(deliveryEventType),
			Status:       string(NotificationDeliveryStatusSending),
			AttemptCount: 1,
		}
		if err := s.repo.CreateNotificationDelivery(ctx, delivery); err != nil {
			return nil, err
		}
	} else {
		if NotificationDeliveryStatus(strings.TrimSpace(delivery.Status)) == NotificationDeliveryStatusSent {
			return &alertPolicyChannelDispatchResult{
				LastDeliveredAt: toUTCTimePointer(delivery.SentAt),
				Successful:      true,
			}, nil
		}
		delivery.ClusterID = strconv.FormatUint(uint64(sourceEvent.ClusterID), 10)
		delivery.ClusterName = strings.TrimSpace(clusterName)
		delivery.PolicyID = policy.ID
		delivery.AlertName = strings.TrimSpace(firstNonEmpty(policy.Name, delivery.AlertName))
		delivery.ChannelName = strings.TrimSpace(channel.Name)
		delivery.EventType = string(deliveryEventType)
		delivery.Status = string(NotificationDeliveryStatusSending)
		delivery.LastError = ""
		delivery.RequestPayload = ""
		delivery.ResponseStatusCode = 0
		delivery.ResponseBodyExcerpt = ""
		delivery.SentAt = nil
		delivery.AttemptCount++
		if delivery.AttemptCount <= 0 {
			delivery.AttemptCount = 1
		}
	}

	attempt, sendErr := sendLocalAlertPolicyNotification(ctx, channel, sourceEvent, recoveryEvent, clusterName, policy, deliveryEventType)
	if attempt != nil {
		delivery.RequestPayload = attempt.RequestPayload
		delivery.ResponseStatusCode = attempt.StatusCode
		delivery.ResponseBodyExcerpt = attempt.ResponseBody
		delivery.SentAt = attempt.SentAt
	}
	if sendErr != nil {
		delivery.Status = string(NotificationDeliveryStatusFailed)
		delivery.LastError = sendErr.Error()
		if err := s.repo.SaveNotificationDelivery(ctx, delivery); err != nil {
			return nil, err
		}
		return &alertPolicyChannelDispatchResult{
			LastError: sendErr.Error(),
		}, sendErr
	}

	delivery.Status = string(NotificationDeliveryStatusSent)
	delivery.LastError = ""
	if err := s.repo.SaveNotificationDelivery(ctx, delivery); err != nil {
		return nil, err
	}
	return &alertPolicyChannelDispatchResult{
		DeliveryCountDelta: 1,
		LastDeliveredAt:    toUTCTimePointer(delivery.SentAt),
		Successful:         true,
	}, nil
}

func sendLocalAlertPolicyNotification(
	ctx context.Context,
	channel *NotificationChannel,
	sourceEvent *monitor.ProcessEvent,
	recoveryEvent *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	deliveryEventType NotificationDeliveryEventType,
) (*notificationSendAttempt, error) {
	payload, err := buildLocalAlertPolicyPayload(channel, sourceEvent, recoveryEvent, clusterName, policy, deliveryEventType)
	if err != nil {
		return nil, err
	}
	return sendNotification(ctx, channel, payload)
}

func buildLocalAlertPolicyPayload(
	channel *NotificationChannel,
	sourceEvent *monitor.ProcessEvent,
	recoveryEvent *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	deliveryEventType NotificationDeliveryEventType,
) (interface{}, error) {
	if channel == nil {
		return nil, fmt.Errorf("notification channel not found")
	}
	if sourceEvent == nil {
		return nil, fmt.Errorf("process event is required")
	}
	if policy == nil {
		return nil, fmt.Errorf("alert policy is required")
	}

	title := buildLocalAlertPolicyMessageTitle(sourceEvent, policy, deliveryEventType)
	message := buildLocalAlertPolicyMessageText(sourceEvent, recoveryEvent, clusterName, policy, deliveryEventType)
	alert := map[string]interface{}{
		"source_type":     AlertSourceTypeLocalProcessEvent,
		"source_key":      buildLocalAlertSourceKey(sourceEvent.ID),
		"event_type":      sourceEvent.EventType,
		"status":          strings.ToLower(string(deliveryEventType)),
		"policy_id":       policy.ID,
		"policy_name":     strings.TrimSpace(policy.Name),
		"policy_type":     policy.PolicyType,
		"template_key":    strings.TrimSpace(policy.TemplateKey),
		"legacy_rule_key": strings.TrimSpace(policy.LegacyRuleKey),
		"severity":        policy.Severity,
		"cluster_id":      strconv.FormatUint(uint64(sourceEvent.ClusterID), 10),
		"cluster_name":    strings.TrimSpace(clusterName),
		"node_id":         sourceEvent.NodeID,
		"host_id":         sourceEvent.HostID,
		"process_name":    strings.TrimSpace(sourceEvent.ProcessName),
		"pid":             sourceEvent.PID,
		"role":            strings.TrimSpace(sourceEvent.Role),
		"event_id":        sourceEvent.ID,
		"fired_at":        sourceEvent.CreatedAt.UTC().Format(time.RFC3339),
		"details":         parseLocalAlertPolicyDetails(sourceEvent.Details),
	}
	if recoveryEvent != nil {
		alert["resolved_at"] = recoveryEvent.CreatedAt.UTC().Format(time.RFC3339)
		alert["resolution_event_id"] = recoveryEvent.ID
		alert["resolution_event_type"] = recoveryEvent.EventType
		alert["recovery_details"] = parseLocalAlertPolicyDetails(recoveryEvent.Details)
	}

	switch channel.Type {
	case NotificationChannelTypeWebhook:
		return map[string]interface{}{
			"title":   title,
			"message": message,
			"alert":   alert,
			"sent_at": time.Now().UTC().Format(time.RFC3339),
		}, nil
	case NotificationChannelTypeWeCom, NotificationChannelTypeDingTalk:
		return map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": message,
			},
		}, nil
	case NotificationChannelTypeFeishu:
		return map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": message,
			},
		}, nil
	case NotificationChannelTypeEmail:
		return &emailNotificationPayload{
			Subject: title,
			Text:    message,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported channel type")
	}
}

func buildLocalAlertPolicyMessageTitle(event *monitor.ProcessEvent, policy *AlertPolicy, deliveryEventType NotificationDeliveryEventType) string {
	severity := strings.ToUpper(string(normalizeAlertPolicySeverity(policy.Severity)))
	if severity == "" {
		severity = "INFO"
	}
	return fmt.Sprintf(
		"[SeaTunnelX][%s][%s] %s",
		strings.ToUpper(string(deliveryEventType)),
		severity,
		strings.TrimSpace(firstNonEmpty(policy.Name, policy.TemplateKey, string(event.EventType), "alert policy")),
	)
}

func buildLocalAlertPolicyMessageText(
	event *monitor.ProcessEvent,
	recoveryEvent *monitor.ProcessEvent,
	clusterName string,
	policy *AlertPolicy,
	deliveryEventType NotificationDeliveryEventType,
) string {
	parts := []string{
		buildLocalAlertPolicyMessageTitle(event, policy, deliveryEventType),
		fmt.Sprintf("Cluster: %s (%d)", strings.TrimSpace(firstNonEmpty(clusterName, "unknown")), event.ClusterID),
		fmt.Sprintf("Policy: %s (%d)", strings.TrimSpace(firstNonEmpty(policy.Name, "unknown")), policy.ID),
		fmt.Sprintf("Event: %s", strings.TrimSpace(firstNonEmpty(string(event.EventType), "unknown"))),
		fmt.Sprintf("FiredAt: %s", event.CreatedAt.UTC().Format(time.RFC3339)),
	}
	if deliveryEventType == NotificationDeliveryEventTypeResolved && recoveryEvent != nil {
		parts = append(parts, fmt.Sprintf("ResolvedAt: %s", recoveryEvent.CreatedAt.UTC().Format(time.RFC3339)))
	}
	if event.EventType == monitor.EventTypeClusterRestartRequested {
		details := parseLocalAlertPolicyDetails(event.Details)
		if payload, ok := details.(map[string]interface{}); ok {
			if operator := strings.TrimSpace(fmt.Sprintf("%v", payload["operator"])); operator != "" && operator != "<nil>" {
				parts = append(parts, fmt.Sprintf("Operator: %s", operator))
			}
			if trigger := strings.TrimSpace(fmt.Sprintf("%v", payload["trigger"])); trigger != "" && trigger != "<nil>" {
				parts = append(parts, fmt.Sprintf("Trigger: %s", trigger))
			}
			if success := strings.TrimSpace(fmt.Sprintf("%v", payload["success"])); success != "" && success != "<nil>" {
				parts = append(parts, fmt.Sprintf("Accepted: %s", success))
			}
			if message := strings.TrimSpace(fmt.Sprintf("%v", payload["message"])); message != "" && message != "<nil>" {
				parts = append(parts, fmt.Sprintf("Result: %s", message))
			}
		}
		if description := strings.TrimSpace(policy.Description); description != "" {
			parts = append(parts, fmt.Sprintf("PolicyDescription: %s", description))
		}
		if details := strings.TrimSpace(event.Details); details != "" {
			parts = append(parts, fmt.Sprintf("EventDetails: %s", details))
		}
		return strings.Join(parts, "\n")
	}
	if event.EventType == monitor.EventTypeNodeOffline {
		details := parseLocalAlertPolicyDetails(event.Details)
		if payload, ok := details.(map[string]interface{}); ok {
			if reason := strings.TrimSpace(fmt.Sprintf("%v", payload["reason"])); reason != "" && reason != "<nil>" {
				parts = append(parts, fmt.Sprintf("Reason: %s", reason))
			}
			if hostName := strings.TrimSpace(fmt.Sprintf("%v", payload["host_name"])); hostName != "" && hostName != "<nil>" {
				parts = append(parts, fmt.Sprintf("Host: %s", hostName))
			}
			if hostIP := strings.TrimSpace(fmt.Sprintf("%v", payload["host_ip"])); hostIP != "" && hostIP != "<nil>" {
				parts = append(parts, fmt.Sprintf("HostIP: %s", hostIP))
			}
			if status := strings.TrimSpace(fmt.Sprintf("%v", payload["node_status"])); status != "" && status != "<nil>" {
				parts = append(parts, fmt.Sprintf("NodeStatus: %s", status))
			}
			if observedSince := strings.TrimSpace(fmt.Sprintf("%v", payload["observed_since"])); observedSince != "" && observedSince != "<nil>" {
				parts = append(parts, fmt.Sprintf("ObservedSince: %s", observedSince))
			}
			if grace := strings.TrimSpace(fmt.Sprintf("%v", payload["grace_seconds"])); grace != "" && grace != "<nil>" {
				parts = append(parts, fmt.Sprintf("GraceSeconds: %s", grace))
			}
		}
		if deliveryEventType == NotificationDeliveryEventTypeResolved && recoveryEvent != nil {
			recoveryDetails := parseLocalAlertPolicyDetails(recoveryEvent.Details)
			if payload, ok := recoveryDetails.(map[string]interface{}); ok {
				if recoveredAt := strings.TrimSpace(fmt.Sprintf("%v", payload["recovered_at"])); recoveredAt != "" && recoveredAt != "<nil>" {
					parts = append(parts, fmt.Sprintf("RecoveredAt: %s", recoveredAt))
				}
				if recoveredStatus := strings.TrimSpace(fmt.Sprintf("%v", payload["node_status"])); recoveredStatus != "" && recoveredStatus != "<nil>" {
					parts = append(parts, fmt.Sprintf("RecoveredNodeStatus: %s", recoveredStatus))
				}
			}
		}
		parts = append(parts,
			fmt.Sprintf("Role: %s", strings.TrimSpace(firstNonEmpty(event.Role, "unknown"))),
			fmt.Sprintf("NodeID: %d", event.NodeID),
			fmt.Sprintf("HostID: %d", event.HostID),
		)
		if description := strings.TrimSpace(policy.Description); description != "" {
			parts = append(parts, fmt.Sprintf("PolicyDescription: %s", description))
		}
		if details := strings.TrimSpace(event.Details); details != "" {
			parts = append(parts, fmt.Sprintf("EventDetails: %s", details))
		}
		return strings.Join(parts, "\n")
	}

	parts = append(parts,
		fmt.Sprintf("Process: %s", strings.TrimSpace(firstNonEmpty(event.ProcessName, "unknown"))),
		fmt.Sprintf("PID: %d", event.PID),
		fmt.Sprintf("Role: %s", strings.TrimSpace(firstNonEmpty(event.Role, "unknown"))),
		fmt.Sprintf("NodeID: %d", event.NodeID),
		fmt.Sprintf("HostID: %d", event.HostID),
	)
	if description := strings.TrimSpace(policy.Description); description != "" {
		parts = append(parts, fmt.Sprintf("PolicyDescription: %s", description))
	}
	if details := strings.TrimSpace(event.Details); details != "" {
		parts = append(parts, fmt.Sprintf("EventDetails: %s", details))
	}
	return strings.Join(parts, "\n")
}

func parseLocalRecoveredOfflineEventID(raw string) uint {
	details := parseLocalAlertPolicyDetails(raw)
	payload, ok := details.(map[string]interface{})
	if !ok {
		return 0
	}

	value := strings.TrimSpace(fmt.Sprintf("%v", payload["offline_event_id"]))
	if value == "" || value == "<nil>" {
		return 0
	}

	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint(id)
}

func parseLocalAlertPolicyDetails(raw string) interface{} {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]interface{}{}
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		return payload
	}
	return trimmed
}

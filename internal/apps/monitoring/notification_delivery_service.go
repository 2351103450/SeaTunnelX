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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ListNotificationDeliveries returns notification delivery history.
// ListNotificationDeliveries 返回通知投递历史。
func (s *Service) ListNotificationDeliveries(ctx context.Context, filter *NotificationDeliveryFilter) (*NotificationDeliveryListData, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if filter == nil {
		filter = &NotificationDeliveryFilter{}
	}
	if filter.Status != "" {
		switch filter.Status {
		case NotificationDeliveryStatusPending,
			NotificationDeliveryStatusSending,
			NotificationDeliveryStatusSent,
			NotificationDeliveryStatusFailed,
			NotificationDeliveryStatusRetrying,
			NotificationDeliveryStatusCanceled:
		default:
			return nil, fmt.Errorf("invalid notification delivery status")
		}
	}
	if filter.EventType != "" {
		switch filter.EventType {
		case NotificationDeliveryEventTypeFiring,
			NotificationDeliveryEventTypeResolved,
			NotificationDeliveryEventTypeTest:
		default:
			return nil, fmt.Errorf("invalid notification delivery event_type")
		}
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 200 {
		filter.PageSize = 200
	}

	rows, total, err := s.repo.ListNotificationDeliveries(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*NotificationDeliveryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toNotificationDeliveryDTO(row))
	}

	return &NotificationDeliveryListData{
		GeneratedAt: time.Now().UTC(),
		Page:        filter.Page,
		PageSize:    filter.PageSize,
		Total:       total,
		Deliveries:  items,
	}, nil
}

func (s *Service) deliverRemoteAlertNotifications(ctx context.Context, record *RemoteAlertRecord) error {
	if s.repo == nil || record == nil {
		return nil
	}

	routes, err := s.repo.ListNotificationRoutes(ctx)
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return nil
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

	sourceKey := buildRemoteAlertSourceKey(record.Fingerprint, record.StartsAt)
	state, err := s.repo.GetAlertStateBySourceKey(ctx, sourceKey)
	if err != nil {
		return err
	}
	currentHandlingStatus := resolveAlertHandlingStatus(state, time.Now().UTC())
	eventType := resolveNotificationDeliveryEventType(record.Status)
	processedChannels := make(map[uint]struct{})

	for _, route := range routes {
		if !matchRemoteAlertRoute(route, record) {
			continue
		}
		if eventType == NotificationDeliveryEventTypeResolved && !route.SendResolved {
			continue
		}
		if currentHandlingStatus == AlertHandlingStatusAcknowledged && route.MuteIfAcknowledged {
			continue
		}
		if currentHandlingStatus == AlertHandlingStatusSilenced && route.MuteIfSilenced {
			continue
		}

		channel := channelMap[route.ChannelID]
		if channel == nil || !channel.Enabled {
			continue
		}
		if _, exists := processedChannels[channel.ID]; exists {
			continue
		}
		if err := s.dispatchRemoteAlertDelivery(ctx, record, channel, eventType); err != nil {
			return err
		}
		processedChannels[channel.ID] = struct{}{}
	}
	return nil
}

func (s *Service) dispatchRemoteAlertDelivery(ctx context.Context, record *RemoteAlertRecord, channel *NotificationChannel, eventType NotificationDeliveryEventType) error {
	if record == nil || channel == nil {
		return nil
	}

	sourceKey := buildRemoteAlertSourceKey(record.Fingerprint, record.StartsAt)
	delivery, err := s.repo.GetNotificationDeliveryByDedupKey(ctx, sourceKey, channel.ID, string(eventType))
	if err != nil {
		return err
	}

	if delivery == nil {
		delivery = &NotificationDelivery{
			AlertID:      sourceKey,
			SourceType:   string(AlertSourceTypeRemoteAlertmanager),
			SourceKey:    sourceKey,
			ClusterID:    strings.TrimSpace(record.ClusterID),
			ClusterName:  strings.TrimSpace(record.ClusterName),
			AlertName:    strings.TrimSpace(record.AlertName),
			ChannelID:    channel.ID,
			ChannelName:  strings.TrimSpace(channel.Name),
			EventType:    string(eventType),
			Status:       string(NotificationDeliveryStatusSending),
			AttemptCount: 1,
		}
		if err := s.repo.CreateNotificationDelivery(ctx, delivery); err != nil {
			return err
		}
	} else {
		if NotificationDeliveryStatus(strings.TrimSpace(delivery.Status)) == NotificationDeliveryStatusSent {
			return nil
		}
		delivery.SourceType = string(AlertSourceTypeRemoteAlertmanager)
		delivery.ClusterID = strings.TrimSpace(record.ClusterID)
		delivery.ClusterName = strings.TrimSpace(record.ClusterName)
		delivery.AlertName = strings.TrimSpace(record.AlertName)
		delivery.ChannelName = strings.TrimSpace(channel.Name)
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
		if err := s.repo.SaveNotificationDelivery(ctx, delivery); err != nil {
			return err
		}
	}

	attempt, sendErr := sendRemoteAlertNotification(ctx, channel, record, eventType)
	if attempt != nil {
		delivery.RequestPayload = attempt.RequestPayload
		delivery.ResponseStatusCode = attempt.StatusCode
		delivery.ResponseBodyExcerpt = attempt.ResponseBody
		delivery.SentAt = attempt.SentAt
	}
	if sendErr != nil {
		delivery.Status = string(NotificationDeliveryStatusFailed)
		delivery.LastError = sendErr.Error()
		return s.repo.SaveNotificationDelivery(ctx, delivery)
	}

	delivery.Status = string(NotificationDeliveryStatusSent)
	delivery.LastError = ""
	return s.repo.SaveNotificationDelivery(ctx, delivery)
}

func sendRemoteAlertNotification(ctx context.Context, channel *NotificationChannel, record *RemoteAlertRecord, eventType NotificationDeliveryEventType) (*notificationSendAttempt, error) {
	payload, err := buildRemoteAlertPayload(channel, record, eventType)
	if err != nil {
		return nil, err
	}
	return sendNotification(ctx, channel, payload)
}

func sendNotification(ctx context.Context, channel *NotificationChannel, payload interface{}) (*notificationSendAttempt, error) {
	if channel == nil {
		return nil, fmt.Errorf("notification channel not found")
	}
	if channel.Type == NotificationChannelTypeEmail {
		return sendEmailNotification(ctx, channel, payload)
	}
	if strings.TrimSpace(channel.Endpoint) == "" {
		return nil, fmt.Errorf("channel endpoint is empty")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(channel.Endpoint), bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &notificationSendAttempt{RequestPayload: string(payloadBytes)}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	body := string(bodyBytes)
	if len(body) > 1000 {
		body = body[:1000]
	}
	now := time.Now().UTC()
	attempt := &notificationSendAttempt{
		RequestPayload: string(payloadBytes),
		StatusCode:     resp.StatusCode,
		ResponseBody:   body,
		SentAt:         &now,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return attempt, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return attempt, nil
}

func buildRemoteAlertPayload(channel *NotificationChannel, record *RemoteAlertRecord, eventType NotificationDeliveryEventType) (interface{}, error) {
	if channel == nil {
		return nil, fmt.Errorf("notification channel not found")
	}
	if record == nil {
		return nil, fmt.Errorf("remote alert record is required")
	}

	title := buildRemoteAlertMessageTitle(record, eventType)
	message := buildRemoteAlertMessageText(record, eventType)
	status := strings.ToUpper(string(eventType))
	startsAt := time.Unix(record.StartsAt, 0).UTC().Format(time.RFC3339)
	resolvedAt := ""
	if record.ResolvedAt != nil {
		resolvedAt = record.ResolvedAt.UTC().Format(time.RFC3339)
	}

	switch channel.Type {
	case NotificationChannelTypeWebhook:
		return map[string]interface{}{
			"title":   title,
			"message": message,
			"alert": map[string]interface{}{
				"source_type":  AlertSourceTypeRemoteAlertmanager,
				"source_key":   buildRemoteAlertSourceKey(record.Fingerprint, record.StartsAt),
				"event_type":   eventType,
				"status":       strings.ToLower(status),
				"alert_name":   strings.TrimSpace(record.AlertName),
				"severity":     strings.TrimSpace(record.Severity),
				"cluster_id":   strings.TrimSpace(record.ClusterID),
				"cluster_name": strings.TrimSpace(record.ClusterName),
				"receiver":     strings.TrimSpace(record.Receiver),
				"summary":      strings.TrimSpace(record.Summary),
				"description":  strings.TrimSpace(record.Description),
				"fingerprint":  strings.TrimSpace(record.Fingerprint),
				"starts_at":    startsAt,
				"resolved_at":  resolvedAt,
			},
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

func buildRemoteAlertMessageTitle(record *RemoteAlertRecord, eventType NotificationDeliveryEventType) string {
	if record == nil {
		return "SeaTunnelX Alert"
	}
	severity := strings.ToUpper(strings.TrimSpace(record.Severity))
	if severity == "" {
		severity = "INFO"
	}
	return fmt.Sprintf("[SeaTunnelX][%s][%s] %s", strings.ToUpper(string(eventType)), severity, strings.TrimSpace(firstNonEmpty(record.AlertName, "alert")))
}

func buildRemoteAlertMessageText(record *RemoteAlertRecord, eventType NotificationDeliveryEventType) string {
	if record == nil {
		return "SeaTunnelX remote alert"
	}

	parts := []string{
		buildRemoteAlertMessageTitle(record, eventType),
		fmt.Sprintf("Cluster: %s (%s)", strings.TrimSpace(firstNonEmpty(record.ClusterName, "unknown")), strings.TrimSpace(firstNonEmpty(record.ClusterID, "unknown"))),
		fmt.Sprintf("Receiver: %s", strings.TrimSpace(firstNonEmpty(record.Receiver, "unknown"))),
		fmt.Sprintf("Summary: %s", strings.TrimSpace(firstNonEmpty(record.Summary, "-"))),
	}
	if description := strings.TrimSpace(record.Description); description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", description))
	}
	parts = append(parts, fmt.Sprintf("Fingerprint: %s", strings.TrimSpace(firstNonEmpty(record.Fingerprint, "unknown"))))
	parts = append(parts, fmt.Sprintf("StartsAt: %s", time.Unix(record.StartsAt, 0).UTC().Format(time.RFC3339)))
	if record.ResolvedAt != nil {
		parts = append(parts, fmt.Sprintf("ResolvedAt: %s", record.ResolvedAt.UTC().Format(time.RFC3339)))
	}
	return strings.Join(parts, "\n")
}

func matchRemoteAlertRoute(route *NotificationRoute, record *RemoteAlertRecord) bool {
	if route == nil || record == nil || !route.Enabled {
		return false
	}
	if sourceType := strings.TrimSpace(route.SourceType); sourceType != "" && !strings.EqualFold(sourceType, string(AlertSourceTypeRemoteAlertmanager)) {
		return false
	}
	if clusterID := strings.TrimSpace(route.ClusterID); clusterID != "" && !strings.EqualFold(clusterID, strings.TrimSpace(record.ClusterID)) {
		return false
	}
	if severity := strings.TrimSpace(route.Severity); severity != "" && !strings.EqualFold(severity, strings.TrimSpace(record.Severity)) {
		return false
	}
	if ruleKey := strings.TrimSpace(route.RuleKey); ruleKey != "" && !strings.EqualFold(ruleKey, strings.TrimSpace(record.AlertName)) {
		return false
	}
	return true
}

func resolveNotificationDeliveryEventType(status string) NotificationDeliveryEventType {
	if strings.EqualFold(strings.TrimSpace(status), string(AlertLifecycleStatusResolved)) {
		return NotificationDeliveryEventTypeResolved
	}
	return NotificationDeliveryEventTypeFiring
}

func toNotificationDeliveryDTO(delivery *NotificationDelivery) *NotificationDeliveryDTO {
	if delivery == nil {
		return nil
	}
	return &NotificationDeliveryDTO{
		ID:                 delivery.ID,
		AlertID:            delivery.AlertID,
		SourceType:         delivery.SourceType,
		SourceKey:          delivery.SourceKey,
		PolicyID:           delivery.PolicyID,
		ClusterID:          delivery.ClusterID,
		ClusterName:        delivery.ClusterName,
		AlertName:          delivery.AlertName,
		ChannelID:          delivery.ChannelID,
		ChannelName:        delivery.ChannelName,
		EventType:          delivery.EventType,
		Status:             delivery.Status,
		AttemptCount:       delivery.AttemptCount,
		LastError:          delivery.LastError,
		ResponseStatusCode: delivery.ResponseStatusCode,
		SentAt:             toUTCTimePointer(delivery.SentAt),
		CreatedAt:          delivery.CreatedAt.UTC(),
		UpdatedAt:          delivery.UpdatedAt.UTC(),
	}
}

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
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type notificationSendAttempt struct {
	RequestPayload string
	StatusCode     int
	ResponseBody   string
	SentAt         *time.Time
}

// ListNotificationRoutes returns all notification routes.
// ListNotificationRoutes 返回全部通知路由。
func (s *Service) ListNotificationRoutes(ctx context.Context) (*NotificationRouteListData, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}

	routes, err := s.repo.ListNotificationRoutes(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*NotificationRouteDTO, 0, len(routes))
	for _, route := range routes {
		items = append(items, toNotificationRouteDTO(route))
	}

	return &NotificationRouteListData{
		GeneratedAt: time.Now().UTC(),
		Total:       len(items),
		Routes:      items,
	}, nil
}

// CreateNotificationRoute creates one notification route.
// CreateNotificationRoute 创建一条通知路由。
func (s *Service) CreateNotificationRoute(ctx context.Context, req *UpsertNotificationRouteRequest) (*NotificationRouteDTO, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if _, err := s.repo.GetNotificationChannelByID(ctx, req.ChannelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("notification channel not found")
		}
		return nil, err
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sendResolved := true
	if req.SendResolved != nil {
		sendResolved = *req.SendResolved
	}
	muteIfAcknowledged := true
	if req.MuteIfAcknowledged != nil {
		muteIfAcknowledged = *req.MuteIfAcknowledged
	}
	muteIfSilenced := true
	if req.MuteIfSilenced != nil {
		muteIfSilenced = *req.MuteIfSilenced
	}

	route := &NotificationRoute{
		Name:               strings.TrimSpace(req.Name),
		Enabled:            enabled,
		SourceType:         strings.TrimSpace(req.SourceType),
		ClusterID:          strings.TrimSpace(req.ClusterID),
		Severity:           strings.ToLower(strings.TrimSpace(req.Severity)),
		RuleKey:            strings.TrimSpace(req.RuleKey),
		ChannelID:          req.ChannelID,
		SendResolved:       sendResolved,
		MuteIfAcknowledged: muteIfAcknowledged,
		MuteIfSilenced:     muteIfSilenced,
	}

	if err := s.repo.CreateNotificationRoute(ctx, route); err != nil {
		return nil, err
	}
	return toNotificationRouteDTO(route), nil
}

// UpdateNotificationRoute updates one notification route.
// UpdateNotificationRoute 更新一条通知路由。
func (s *Service) UpdateNotificationRoute(ctx context.Context, id uint, req *UpsertNotificationRouteRequest) (*NotificationRouteDTO, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if id == 0 {
		return nil, fmt.Errorf("invalid route id")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	route, err := s.repo.GetNotificationRouteByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("notification route not found")
		}
		return nil, err
	}
	if _, err := s.repo.GetNotificationChannelByID(ctx, req.ChannelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("notification channel not found")
		}
		return nil, err
	}

	route.Name = strings.TrimSpace(req.Name)
	route.SourceType = strings.TrimSpace(req.SourceType)
	route.ClusterID = strings.TrimSpace(req.ClusterID)
	route.Severity = strings.ToLower(strings.TrimSpace(req.Severity))
	route.RuleKey = strings.TrimSpace(req.RuleKey)
	route.ChannelID = req.ChannelID
	if req.Enabled != nil {
		route.Enabled = *req.Enabled
	}
	if req.SendResolved != nil {
		route.SendResolved = *req.SendResolved
	}
	if req.MuteIfAcknowledged != nil {
		route.MuteIfAcknowledged = *req.MuteIfAcknowledged
	}
	if req.MuteIfSilenced != nil {
		route.MuteIfSilenced = *req.MuteIfSilenced
	}

	if err := s.repo.SaveNotificationRoute(ctx, route); err != nil {
		return nil, err
	}
	return toNotificationRouteDTO(route), nil
}

// DeleteNotificationRoute deletes one notification route.
// DeleteNotificationRoute 删除一条通知路由。
func (s *Service) DeleteNotificationRoute(ctx context.Context, id uint) error {
	if s.repo == nil {
		return fmt.Errorf("monitoring repository is not configured")
	}
	if id == 0 {
		return fmt.Errorf("invalid route id")
	}
	return s.repo.DeleteNotificationRoute(ctx, id)
}

// TestNotificationChannel performs one test send against a notification channel.
// TestNotificationChannel 对指定通知渠道执行一次测试发送。
func (s *Service) TestNotificationChannel(ctx context.Context, id uint) (*NotificationChannelTestResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if id == 0 {
		return nil, fmt.Errorf("invalid channel id")
	}

	channel, err := s.repo.GetNotificationChannelByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("notification channel not found")
		}
		return nil, err
	}

	delivery := &NotificationDelivery{
		AlertID:      fmt.Sprintf("test-channel-%d", channel.ID),
		SourceType:   "system_test",
		SourceKey:    fmt.Sprintf("test:channel:%d:%d", channel.ID, time.Now().UTC().UnixNano()),
		AlertName:    "SeaTunnelX notification test",
		ChannelID:    channel.ID,
		ChannelName:  strings.TrimSpace(channel.Name),
		EventType:    string(NotificationDeliveryEventTypeTest),
		Status:       string(NotificationDeliveryStatusSending),
		AttemptCount: 1,
	}
	if err := s.repo.CreateNotificationDelivery(ctx, delivery); err != nil {
		return nil, err
	}

	attempt, sendErr := sendTestNotification(ctx, channel)
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
		return &NotificationChannelTestResult{
			ChannelID:    channel.ID,
			DeliveryID:   delivery.ID,
			Status:       delivery.Status,
			SentAt:       delivery.SentAt,
			LastError:    delivery.LastError,
			StatusCode:   delivery.ResponseStatusCode,
			ResponseBody: delivery.ResponseBodyExcerpt,
		}, nil
	}

	delivery.Status = string(NotificationDeliveryStatusSent)
	if err := s.repo.SaveNotificationDelivery(ctx, delivery); err != nil {
		return nil, err
	}
	return &NotificationChannelTestResult{
		ChannelID:    channel.ID,
		DeliveryID:   delivery.ID,
		Status:       delivery.Status,
		SentAt:       delivery.SentAt,
		StatusCode:   delivery.ResponseStatusCode,
		ResponseBody: delivery.ResponseBodyExcerpt,
	}, nil
}

func toNotificationRouteDTO(route *NotificationRoute) *NotificationRouteDTO {
	if route == nil {
		return nil
	}
	return &NotificationRouteDTO{
		ID:                 route.ID,
		Name:               route.Name,
		Enabled:            route.Enabled,
		SourceType:         route.SourceType,
		ClusterID:          route.ClusterID,
		Severity:           route.Severity,
		RuleKey:            route.RuleKey,
		ChannelID:          route.ChannelID,
		SendResolved:       route.SendResolved,
		MuteIfAcknowledged: route.MuteIfAcknowledged,
		MuteIfSilenced:     route.MuteIfSilenced,
		CreatedAt:          route.CreatedAt,
		UpdatedAt:          route.UpdatedAt,
	}
}

func sendTestNotification(ctx context.Context, channel *NotificationChannel) (*notificationSendAttempt, error) {
	payload, err := buildTestPayload(channel)
	if err != nil {
		return nil, err
	}
	return sendNotification(ctx, channel, payload)
}

func buildTestPayload(channel *NotificationChannel) (interface{}, error) {
	if channel == nil {
		return nil, fmt.Errorf("notification channel not found")
	}

	message := "This is a test notification from SeaTunnelX."
	switch channel.Type {
	case NotificationChannelTypeWebhook:
		return map[string]interface{}{
			"title":        "SeaTunnelX notification test",
			"message":      message,
			"channel_name": channel.Name,
			"channel_type": channel.Type,
			"sent_at":      time.Now().UTC().Format(time.RFC3339),
		}, nil
	case NotificationChannelTypeWeCom, NotificationChannelTypeDingTalk:
		return map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": fmt.Sprintf("[SeaTunnelX] %s", message),
			},
		}, nil
	case NotificationChannelTypeFeishu:
		return map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": fmt.Sprintf("[SeaTunnelX] %s", message),
			},
		}, nil
	case NotificationChannelTypeEmail:
		return &emailNotificationPayload{
			Subject: "SeaTunnelX notification test",
			Text:    fmt.Sprintf("[SeaTunnelX] %s", message),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported channel type")
	}
}

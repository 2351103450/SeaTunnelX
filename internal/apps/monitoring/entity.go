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

import "time"

// AlertRule represents persistent alert rule configuration.
// AlertRule 表示持久化的告警规则配置。
type AlertRule struct {
	ID            uint          `json:"id" gorm:"primaryKey;autoIncrement"`
	ClusterID     uint          `json:"cluster_id" gorm:"not null;index;uniqueIndex:ux_monitoring_cluster_rule"`
	RuleKey       string        `json:"rule_key" gorm:"size:80;not null;uniqueIndex:ux_monitoring_cluster_rule"`
	RuleName      string        `json:"rule_name" gorm:"size:120;not null"`
	Description   string        `json:"description" gorm:"type:text"`
	Severity      AlertSeverity `json:"severity" gorm:"size:20;default:warning"`
	Enabled       bool          `json:"enabled" gorm:"default:true"`
	Threshold     int           `json:"threshold" gorm:"default:1"`
	WindowSeconds int           `json:"window_seconds" gorm:"default:300"`
	CreatedAt     time.Time     `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt     time.Time     `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName specifies the table name for AlertRule.
// TableName 指定 AlertRule 表名。
func (AlertRule) TableName() string {
	return "monitoring_alert_rules"
}

// AlertEventState records manual state operations for one process event.
// AlertEventState 记录针对单条进程事件的人工操作状态。
type AlertEventState struct {
	ID             uint        `json:"id" gorm:"primaryKey;autoIncrement"`
	EventID        uint        `json:"event_id" gorm:"not null;uniqueIndex"`
	ClusterID      uint        `json:"cluster_id" gorm:"not null;index"`
	Status         AlertStatus `json:"status" gorm:"size:20;not null;index"`
	AcknowledgedBy string      `json:"acknowledged_by" gorm:"size:100"`
	AcknowledgedAt *time.Time  `json:"acknowledged_at"`
	SilencedBy     string      `json:"silenced_by" gorm:"size:100"`
	SilencedUntil  *time.Time  `json:"silenced_until" gorm:"index"`
	Note           string      `json:"note" gorm:"type:text"`
	CreatedAt      time.Time   `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time   `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName specifies the table name for AlertEventState.
// TableName 指定 AlertEventState 表名。
func (AlertEventState) TableName() string {
	return "monitoring_alert_event_states"
}

// NotificationChannelType represents notification channel type.
// NotificationChannelType 表示通知渠道类型。
type NotificationChannelType string

const (
	// NotificationChannelTypeWebhook indicates generic webhook.
	// NotificationChannelTypeWebhook 表示通用 Webhook。
	NotificationChannelTypeWebhook NotificationChannelType = "webhook"
	// NotificationChannelTypeEmail indicates email notification channel.
	// NotificationChannelTypeEmail 表示邮件通知渠道。
	NotificationChannelTypeEmail NotificationChannelType = "email"
	// NotificationChannelTypeWeCom indicates WeCom (企业微信) webhook channel.
	// NotificationChannelTypeWeCom 表示企业微信渠道。
	NotificationChannelTypeWeCom NotificationChannelType = "wecom"
	// NotificationChannelTypeDingTalk indicates DingTalk webhook channel.
	// NotificationChannelTypeDingTalk 表示钉钉渠道。
	NotificationChannelTypeDingTalk NotificationChannelType = "dingtalk"
	// NotificationChannelTypeFeishu indicates Feishu webhook channel.
	// NotificationChannelTypeFeishu 表示飞书渠道。
	NotificationChannelTypeFeishu NotificationChannelType = "feishu"
)

// NotificationChannel represents one alert notification channel.
// NotificationChannel 表示一条告警通知渠道配置。
type NotificationChannel struct {
	ID          uint                    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string                  `json:"name" gorm:"size:120;not null;index"`
	Type        NotificationChannelType `json:"type" gorm:"size:30;not null;index"`
	Enabled     bool                    `json:"enabled" gorm:"default:true"`
	Endpoint    string                  `json:"endpoint" gorm:"size:500"`
	Secret      string                  `json:"secret" gorm:"size:500"`
	Description string                  `json:"description" gorm:"type:text"`
	CreatedAt   time.Time               `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time               `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName specifies the table name for NotificationChannel.
// TableName 指定 NotificationChannel 表名。
func (NotificationChannel) TableName() string {
	return "monitoring_notification_channels"
}

// RemoteAlertRecord stores one Alertmanager webhook alert after normalization.
// RemoteAlertRecord 保存标准化后的 Alertmanager webhook 告警记录。
type RemoteAlertRecord struct {
	ID uint `json:"id" gorm:"primaryKey;autoIncrement"`

	Fingerprint string `json:"fingerprint" gorm:"size:255;not null;index;uniqueIndex:ux_monitoring_remote_alert"`
	StartsAt    int64  `json:"starts_at" gorm:"not null;index;uniqueIndex:ux_monitoring_remote_alert"`

	Status      string `json:"status" gorm:"size:32;index"`
	Receiver    string `json:"receiver" gorm:"size:200"`
	AlertName   string `json:"alert_name" gorm:"size:255;index"`
	Severity    string `json:"severity" gorm:"size:64;index"`
	ClusterID   string `json:"cluster_id" gorm:"size:64;index"`
	ClusterName string `json:"cluster_name" gorm:"size:255;index"`
	Env         string `json:"env" gorm:"size:64;index"`

	GeneratorURL string `json:"generator_url" gorm:"size:1024"`
	Summary      string `json:"summary" gorm:"type:text"`
	Description  string `json:"description" gorm:"type:text"`

	LabelsJSON      string `json:"labels_json" gorm:"type:text"`
	AnnotationsJSON string `json:"annotations_json" gorm:"type:text"`

	EndsAt         int64      `json:"ends_at"`
	ResolvedAt     *time.Time `json:"resolved_at"`
	LastReceivedAt time.Time  `json:"last_received_at" gorm:"index"`
	CreatedAt      time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName specifies table name for RemoteAlertRecord.
// TableName 指定 RemoteAlertRecord 表名。
func (RemoteAlertRecord) TableName() string {
	return "monitoring_remote_alerts"
}

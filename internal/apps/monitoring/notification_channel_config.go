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
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeNotificationChannelConfig(channelType NotificationChannelType, config *NotificationChannelConfig) (*NotificationChannelConfig, error) {
	if channelType != NotificationChannelTypeEmail {
		return nil, nil
	}
	if config == nil || config.Email == nil {
		return nil, fmt.Errorf("email config is required")
	}
	normalizedEmail := config.Email.Normalize()
	if err := normalizedEmail.Validate(); err != nil {
		return nil, err
	}
	return &NotificationChannelConfig{Email: normalizedEmail}, nil
}

func marshalNotificationChannelConfig(channelType NotificationChannelType, config *NotificationChannelConfig) (string, error) {
	normalized, err := normalizeNotificationChannelConfig(channelType, config)
	if err != nil {
		return "", err
	}
	if normalized == nil {
		return "", nil
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func unmarshalNotificationChannelConfig(channelType NotificationChannelType, raw string) *NotificationChannelConfig {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || channelType != NotificationChannelTypeEmail {
		return nil
	}

	var config NotificationChannelConfig
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return nil
	}
	if config.Email == nil {
		return nil
	}
	return &NotificationChannelConfig{Email: config.Email.Normalize()}
}

func deriveNotificationChannelEndpoint(channelType NotificationChannelType, endpoint string, config *NotificationChannelConfig) string {
	if channelType != NotificationChannelTypeEmail {
		return strings.TrimSpace(endpoint)
	}
	if config == nil || config.Email == nil {
		return strings.TrimSpace(endpoint)
	}
	return fmt.Sprintf("%s:%d", config.Email.Host, config.Email.Port)
}

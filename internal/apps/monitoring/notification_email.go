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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// emailNotificationPayload represents one normalized email message.
// emailNotificationPayload 表示一份规范化邮件消息。
type emailNotificationPayload struct {
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	To      []string `json:"to,omitempty"`
}

func sendEmailNotification(ctx context.Context, channel *NotificationChannel, payload interface{}) (*notificationSendAttempt, error) {
	if channel == nil {
		return nil, fmt.Errorf("notification channel not found")
	}
	config := unmarshalNotificationChannelConfig(channel.Type, channel.ConfigJSON)
	if config == nil || config.Email == nil {
		return nil, fmt.Errorf("email config is not configured")
	}
	emailConfig := config.Email.Normalize()
	if err := emailConfig.Validate(); err != nil {
		return nil, err
	}

	emailPayload, err := normalizeEmailNotificationPayload(payload, emailConfig.Recipients)
	if err != nil {
		return nil, err
	}
	messageBytes, err := buildSMTPMessage(emailConfig, emailPayload)
	if err != nil {
		return nil, err
	}

	requestPayloadBytes, _ := json.Marshal(map[string]interface{}{
		"subject":    emailPayload.Subject,
		"to":         emailPayload.To,
		"from":       emailConfig.From,
		"smtp_host":  emailConfig.Host,
		"smtp_port":  emailConfig.Port,
		"smtp_mode":  emailConfig.Security,
		"channel_id": channel.ID,
	})

	if err := deliverSMTPMessage(ctx, emailConfig, emailPayload.To, messageBytes); err != nil {
		return &notificationSendAttempt{RequestPayload: string(requestPayloadBytes)}, err
	}

	now := time.Now().UTC()
	return &notificationSendAttempt{
		RequestPayload: string(requestPayloadBytes),
		StatusCode:     250,
		ResponseBody:   "smtp accepted",
		SentAt:         &now,
	}, nil
}

func normalizeEmailNotificationPayload(payload interface{}, defaultRecipients []string) (*emailNotificationPayload, error) {
	switch typed := payload.(type) {
	case *emailNotificationPayload:
		if typed == nil {
			return nil, fmt.Errorf("email payload is required")
		}
		cloned := *typed
		cloned.Subject = strings.TrimSpace(cloned.Subject)
		cloned.Text = strings.TrimSpace(cloned.Text)
		cloned.To = normalizeEmailRecipients(cloned.To)
		if len(cloned.To) == 0 {
			cloned.To = normalizeEmailRecipients(defaultRecipients)
		}
		if cloned.Subject == "" {
			return nil, fmt.Errorf("email subject is required")
		}
		if cloned.Text == "" {
			return nil, fmt.Errorf("email text is required")
		}
		if len(cloned.To) == 0 {
			return nil, fmt.Errorf("email recipients are required")
		}
		return &cloned, nil
	case emailNotificationPayload:
		return normalizeEmailNotificationPayload(&typed, defaultRecipients)
	default:
		return nil, fmt.Errorf("unsupported email payload")
	}
}

func buildSMTPMessage(config *NotificationChannelEmailConfig, payload *emailNotificationPayload) ([]byte, error) {
	if config == nil || payload == nil {
		return nil, fmt.Errorf("email config and payload are required")
	}

	var body bytes.Buffer
	qp := quotedprintable.NewWriter(&body)
	if _, err := qp.Write([]byte(payload.Text)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}

	subject := mime.QEncoding.Encode("utf-8", strings.TrimSpace(payload.Subject))
	headers := []string{
		fmt.Sprintf("From: %s", strings.TrimSpace(config.From)),
		fmt.Sprintf("To: %s", strings.Join(payload.To, ", ")),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	}

	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body.String()
	return []byte(message), nil
}

func deliverSMTPMessage(ctx context.Context, config *NotificationChannelEmailConfig, recipients []string, message []byte) error {
	if config == nil {
		return fmt.Errorf("email config is required")
	}
	if len(recipients) == 0 {
		return fmt.Errorf("email recipients are required")
	}
	addr := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var (
		client *smtp.Client
		conn   net.Conn
		err    error
	)

	switch config.Security {
	case NotificationEmailSecuritySSL:
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: strings.TrimSpace(config.Host),
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return err
		}
		client, err = smtp.NewClient(conn, strings.TrimSpace(config.Host))
		if err != nil {
			_ = conn.Close()
			return err
		}
	default:
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		client, err = smtp.NewClient(conn, strings.TrimSpace(config.Host))
		if err != nil {
			_ = conn.Close()
			return err
		}
		if config.Security == NotificationEmailSecurityStartTLS {
			ok, _ := client.Extension("STARTTLS")
			if !ok {
				_ = client.Close()
				return fmt.Errorf("smtp server does not support STARTTLS")
			}
			if err := client.StartTLS(&tls.Config{
				ServerName: strings.TrimSpace(config.Host),
				MinVersion: tls.VersionTLS12,
			}); err != nil {
				_ = client.Close()
				return err
			}
		}
	}
	defer func() {
		_ = client.Quit()
		_ = client.Close()
	}()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	if strings.TrimSpace(config.Username) != "" {
		ok, _ := client.Extension("AUTH")
		if !ok {
			return fmt.Errorf("smtp server does not support AUTH")
		}
		auth := smtp.PlainAuth("", strings.TrimSpace(config.Username), config.Password, strings.TrimSpace(config.Host))
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(strings.TrimSpace(config.From)); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(strings.TrimSpace(recipient)); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return nil
}

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
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSMTPServer struct {
	listener   net.Listener
	wg         sync.WaitGroup
	mu         sync.Mutex
	mailFrom   string
	recipients []string
	data       string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp server: %v", err)
	}
	server := &fakeSMTPServer{listener: listener}
	server.wg.Add(1)
	go server.serve(t)
	return server
}

func (s *fakeSMTPServer) addr() string {
	return s.listener.Addr().String()
}

func (s *fakeSMTPServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func (s *fakeSMTPServer) serve(t *testing.T) {
	defer s.wg.Done()
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeLine := func(line string) {
		_, _ = writer.WriteString(line + "\r\n")
		_ = writer.Flush()
	}
	writeLine("220 fake-smtp ready")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "EHLO") || strings.HasPrefix(trimmed, "HELO"):
			_, _ = writer.WriteString("250-fake-smtp\r\n")
			_, _ = writer.WriteString("250 OK\r\n")
			_ = writer.Flush()
		case strings.HasPrefix(trimmed, "MAIL FROM:"):
			s.mu.Lock()
			s.mailFrom = strings.TrimPrefix(trimmed, "MAIL FROM:")
			s.mu.Unlock()
			writeLine("250 OK")
		case strings.HasPrefix(trimmed, "RCPT TO:"):
			s.mu.Lock()
			s.recipients = append(s.recipients, strings.TrimPrefix(trimmed, "RCPT TO:"))
			s.mu.Unlock()
			writeLine("250 OK")
		case trimmed == "DATA":
			writeLine("354 End data with <CR><LF>.<CR><LF>")
			var dataLines []string
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				dataLines = append(dataLines, dataLine)
			}
			s.mu.Lock()
			s.data = strings.Join(dataLines, "")
			s.mu.Unlock()
			writeLine("250 Accepted")
		case trimmed == "QUIT":
			writeLine("221 Bye")
			return
		default:
			t.Logf("unhandled smtp line: %s", trimmed)
			writeLine("250 OK")
		}
	}
}

func TestSendEmailNotification_SMTPPlain(t *testing.T) {
	server := newFakeSMTPServer(t)
	defer server.close()

	host, portRaw, err := net.SplitHostPort(server.addr())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portRaw, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	configJSON, err := marshalNotificationChannelConfig(NotificationChannelTypeEmail, &NotificationChannelConfig{
		Email: &NotificationChannelEmailConfig{
			Protocol:   "smtp",
			Security:   NotificationEmailSecurityNone,
			Host:       host,
			Port:       port,
			From:       "alerts@example.com",
			Recipients: []string{"ops@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("marshal channel config: %v", err)
	}

	attempt, err := sendEmailNotification(context.Background(), &NotificationChannel{
		ID:         100,
		Name:       "email-demo",
		Type:       NotificationChannelTypeEmail,
		ConfigJSON: configJSON,
	}, &emailNotificationPayload{
		Subject: "SeaTunnelX restart alert",
		Text:    "cluster restart requested",
	})
	if err != nil {
		t.Fatalf("send email notification: %v", err)
	}
	if attempt == nil || attempt.StatusCode != 250 {
		t.Fatalf("unexpected attempt: %+v", attempt)
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	if !strings.Contains(server.mailFrom, "alerts@example.com") {
		t.Fatalf("unexpected mail from: %s", server.mailFrom)
	}
	if len(server.recipients) != 1 || !strings.Contains(server.recipients[0], "ops@example.com") {
		t.Fatalf("unexpected recipients: %+v", server.recipients)
	}
	if !strings.Contains(server.data, "Subject: ") || !strings.Contains(server.data, "SeaTunnelX restart alert") {
		t.Fatalf("unexpected message subject: %s", server.data)
	}
	if !strings.Contains(server.data, "cluster restart requested") {
		t.Fatalf("unexpected message body: %s", server.data)
	}
}

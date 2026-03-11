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

package diagnostics

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newDiagnosticsTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&SeatunnelErrorGroup{}, &SeatunnelErrorEvent{}, &SeatunnelLogCursor{}))

	repo := NewRepository(database)
	return NewServiceWithRepository(repo, nil, nil, nil), database
}

func TestIngestSeatunnelErrorGroupsRepeatedFingerprints(t *testing.T) {
	service, database := newDiagnosticsTestService(t)
	now := time.Now().Add(-5 * time.Minute).UTC()

	req1 := &IngestSeatunnelErrorRequest{
		ClusterID:   1,
		NodeID:      11,
		HostID:      21,
		AgentID:     "agent-a",
		Role:        "worker",
		InstallDir:  "/opt/seatunnel-a",
		SourceFile:  "/opt/seatunnel-a/logs/job-100.log",
		SourceKind:  "job",
		JobID:       "100",
		OccurredAt:  now,
		Message:     "org.apache.seatunnel.engine.server.exception.TaskExecuteException: job 100 failed on 10.0.0.1",
		Evidence:    "org.apache.seatunnel.engine.server.exception.TaskExecuteException: job 100 failed on 10.0.0.1\nat org.apache.seatunnel.Engine.run(Engine.java:100)",
		CursorStart: 100,
		CursorEnd:   220,
	}
	req2 := &IngestSeatunnelErrorRequest{
		ClusterID:   2,
		NodeID:      12,
		HostID:      22,
		AgentID:     "agent-b",
		Role:        "worker",
		InstallDir:  "/opt/seatunnel-b",
		SourceFile:  "/opt/seatunnel-b/logs/job-200.log",
		SourceKind:  "job",
		JobID:       "200",
		OccurredAt:  now.Add(2 * time.Minute),
		Message:     "org.apache.seatunnel.engine.server.exception.TaskExecuteException: job 200 failed on 10.0.0.2",
		Evidence:    "org.apache.seatunnel.engine.server.exception.TaskExecuteException: job 200 failed on 10.0.0.2\nat org.apache.seatunnel.Engine.run(Engine.java:200)",
		CursorStart: 220,
		CursorEnd:   360,
	}

	require.NoError(t, service.IngestSeatunnelError(t.Context(), req1))
	require.NoError(t, service.IngestSeatunnelError(t.Context(), req2))

	var groupCount int64
	require.NoError(t, database.Model(&SeatunnelErrorGroup{}).Count(&groupCount).Error)
	assert.Equal(t, int64(1), groupCount)

	var eventCount int64
	require.NoError(t, database.Model(&SeatunnelErrorEvent{}).Count(&eventCount).Error)
	assert.Equal(t, int64(2), eventCount)

	var group SeatunnelErrorGroup
	require.NoError(t, database.First(&group).Error)
	assert.Equal(t, int64(2), group.OccurrenceCount)
	assert.Equal(t, uint(2), group.LastClusterID)
	assert.False(t, group.FirstSeenAt.After(group.LastSeenAt))
	assert.Contains(t, group.ExceptionClass, "TaskExecuteException")
}

func TestIngestSeatunnelErrorSkipsDuplicateCursor(t *testing.T) {
	service, database := newDiagnosticsTestService(t)
	req := &IngestSeatunnelErrorRequest{
		ClusterID:   1,
		NodeID:      11,
		HostID:      21,
		AgentID:     "agent-a",
		Role:        "worker",
		InstallDir:  "/opt/seatunnel-a",
		SourceFile:  "/opt/seatunnel-a/logs/seatunnel-engine-worker.log",
		SourceKind:  "engine",
		OccurredAt:  time.Now().UTC(),
		Message:     "java.lang.IllegalStateException: cluster unhealthy",
		Evidence:    "java.lang.IllegalStateException: cluster unhealthy\nat org.apache.seatunnel.Engine.run(Engine.java:100)",
		CursorStart: 10,
		CursorEnd:   88,
	}

	require.NoError(t, service.IngestSeatunnelError(t.Context(), req))
	require.NoError(t, service.IngestSeatunnelError(t.Context(), req))

	var eventCount int64
	require.NoError(t, database.Model(&SeatunnelErrorEvent{}).Count(&eventCount).Error)
	assert.Equal(t, int64(1), eventCount)

	var group SeatunnelErrorGroup
	require.NoError(t, database.First(&group).Error)
	assert.Equal(t, int64(1), group.OccurrenceCount)

	var cursor SeatunnelLogCursor
	require.NoError(t, database.First(&cursor).Error)
	assert.Equal(t, int64(88), cursor.CursorOffset)
}

func TestIngestSeatunnelErrorAcceptsCursorResetAfterRotation(t *testing.T) {
	service, database := newDiagnosticsTestService(t)
	baseTime := time.Now().UTC()

	first := &IngestSeatunnelErrorRequest{
		ClusterID:   1,
		NodeID:      11,
		HostID:      21,
		AgentID:     "agent-a",
		Role:        "worker",
		InstallDir:  "/opt/seatunnel-a",
		SourceFile:  "/opt/seatunnel-a/logs/seatunnel-engine-worker.log",
		SourceKind:  "engine",
		OccurredAt:  baseTime,
		Message:     "java.lang.IllegalStateException: before rotation",
		Evidence:    "java.lang.IllegalStateException: before rotation\nat org.apache.seatunnel.Engine.run(Engine.java:100)",
		CursorStart: 10,
		CursorEnd:   1000,
	}
	second := &IngestSeatunnelErrorRequest{
		ClusterID:   1,
		NodeID:      11,
		HostID:      21,
		AgentID:     "agent-a",
		Role:        "worker",
		InstallDir:  "/opt/seatunnel-a",
		SourceFile:  "/opt/seatunnel-a/logs/seatunnel-engine-worker.log",
		SourceKind:  "engine",
		OccurredAt:  baseTime.Add(time.Minute),
		Message:     "java.lang.IllegalArgumentException: after rotation",
		Evidence:    "java.lang.IllegalArgumentException: after rotation\nat org.apache.seatunnel.Engine.run(Engine.java:200)",
		CursorStart: 0,
		CursorEnd:   120,
	}

	require.NoError(t, service.IngestSeatunnelError(t.Context(), first))
	require.NoError(t, service.IngestSeatunnelError(t.Context(), second))

	var eventCount int64
	require.NoError(t, database.Model(&SeatunnelErrorEvent{}).Count(&eventCount).Error)
	assert.Equal(t, int64(2), eventCount)

	var cursor SeatunnelLogCursor
	require.NoError(t, database.First(&cursor).Error)
	assert.Equal(t, int64(120), cursor.CursorOffset)
}

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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/seatunnel/seatunnelX/internal/apps/cluster"
	appconfig "github.com/seatunnel/seatunnelX/internal/apps/config"
	"github.com/seatunnel/seatunnelX/internal/apps/monitor"
	monitoringapp "github.com/seatunnel/seatunnelX/internal/apps/monitoring"
	"gorm.io/gorm"
)

type fakeDiagnosticProcessEventReader struct {
	filteredRows      []*monitor.ProcessEventWithHost
	filteredErr       error
	clusterEvents     []*monitor.ProcessEvent
	clusterEventsErr  error
	listEventsCalls   int
	clusterEventCalls int
}

func (f *fakeDiagnosticProcessEventReader) ListEvents(_ context.Context, _ *monitor.ProcessEventFilter) ([]*monitor.ProcessEventWithHost, int64, error) {
	f.listEventsCalls++
	if f.filteredErr != nil {
		return nil, 0, f.filteredErr
	}
	return f.filteredRows, int64(len(f.filteredRows)), nil
}

func (f *fakeDiagnosticProcessEventReader) ListClusterEvents(_ context.Context, _ uint, _ int) ([]*monitor.ProcessEvent, error) {
	f.clusterEventCalls++
	if f.clusterEventsErr != nil {
		return nil, f.clusterEventsErr
	}
	return f.clusterEvents, nil
}

func TestResolveDiagnosticCollectionWindow_prefersTaskLookbackAndInspectionFinishAt(t *testing.T) {
	finishedAt := time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC)
	task := &DiagnosticTask{LookbackMinutes: 90}
	detail := &ClusterInspectionReportDetailData{
		Report: &ClusterInspectionReportInfo{
			LookbackMinutes: 30,
			FinishedAt:      &finishedAt,
		},
	}

	window := resolveDiagnosticCollectionWindow(task, detail)

	if window.LookbackMinutes != 90 {
		t.Fatalf("expected lookback 90, got %d", window.LookbackMinutes)
	}
	if !window.End.Equal(finishedAt) {
		t.Fatalf("expected end %s, got %s", finishedAt, window.End)
	}
	expectedStart := finishedAt.Add(-90 * time.Minute)
	if !window.Start.Equal(expectedStart) {
		t.Fatalf("expected start %s, got %s", expectedStart, window.Start)
	}
}

func TestFilterDiagnosticAlertsByWindow_includesOverlapResolvedAndExplicitSource(t *testing.T) {
	start := time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	resolvedAt := start.Add(5 * time.Minute)
	closedAt := start.Add(10 * time.Minute)

	alerts := []*monitoringapp.AlertInstance{
		{
			AlertID:    "firing-overlap",
			Status:     monitoringapp.AlertDisplayStatusFiring,
			FiringAt:   start.Add(-1 * time.Hour),
			LastSeenAt: start.Add(2 * time.Minute),
		},
		{
			AlertID:    "resolved-in-window",
			Status:     monitoringapp.AlertDisplayStatusResolved,
			FiringAt:   start.Add(-10 * time.Minute),
			LastSeenAt: resolvedAt,
			ResolvedAt: &resolvedAt,
		},
		{
			AlertID:    "closed-in-window",
			Status:     monitoringapp.AlertDisplayStatusClosed,
			FiringAt:   start.Add(-10 * time.Minute),
			LastSeenAt: closedAt,
			ClosedAt:   &closedAt,
		},
		{
			AlertID:    "stale-firing",
			Status:     monitoringapp.AlertDisplayStatusFiring,
			FiringAt:   start.Add(-2 * time.Hour),
			LastSeenAt: start.Add(-1 * time.Minute),
		},
		{
			AlertID:    "explicit-source",
			Status:     monitoringapp.AlertDisplayStatusClosed,
			FiringAt:   start.Add(-24 * time.Hour),
			LastSeenAt: start.Add(-24 * time.Hour),
		},
	}

	filtered := filterDiagnosticAlertsByWindow(alerts, start, end, "explicit-source")
	gotIDs := make([]string, 0, len(filtered))
	for _, item := range filtered {
		if item != nil {
			gotIDs = append(gotIDs, item.AlertID)
		}
	}

	expected := []string{"firing-overlap", "resolved-in-window", "explicit-source"}
	if len(gotIDs) != len(expected) {
		t.Fatalf("expected %d alerts, got %d: %v", len(expected), len(gotIDs), gotIDs)
	}
	for _, id := range expected {
		if !containsString(gotIDs, id) {
			t.Fatalf("expected alert %q in filtered list, got %v", id, gotIDs)
		}
	}
	if containsString(gotIDs, "closed-in-window") || containsString(gotIDs, "stale-firing") {
		t.Fatalf("unexpected alerts in filtered list: %v", gotIDs)
	}
}

func TestBuildDiagnosticBundleManifest_compactsMetadata(t *testing.T) {
	start := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	task := &DiagnosticTask{
		ID:              12,
		ClusterID:       7,
		TriggerSource:   DiagnosticTaskSourceInspectionFinding,
		SourceRef:       DiagnosticTaskSourceRef{InspectionReportID: 3, InspectionFindingID: 4},
		Options:         DiagnosticTaskOptions{IncludeThreadDump: true}.Normalize(),
		Status:          DiagnosticTaskStatusSucceeded,
		Summary:         "diagnostic summary",
		LookbackMinutes: 30,
		CreatedBy:       99,
		CreatedByName:   "tester",
		StartedAt:       timePtr(start),
		CompletedAt:     timePtr(end),
	}
	state := &diagnosticBundleExecutionState{
		WindowStart:     timePtr(start),
		WindowEnd:       timePtr(end),
		LookbackMinutes: 30,
	}

	manifest := buildDiagnosticBundleManifest(task, []*diagnosticBundleArtifact{}, state)
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	text := string(payload)

	if !strings.Contains(text, `"lookback_minutes":30`) {
		t.Fatalf("expected lookback_minutes in manifest, got %s", text)
	}
	if !strings.Contains(text, `"window_start"`) || !strings.Contains(text, `"window_end"`) {
		t.Fatalf("expected window range in manifest, got %s", text)
	}
	if strings.Contains(text, `"created_by"`) || strings.Contains(text, `"created_by_name"`) {
		t.Fatalf("creator metadata should not be present in manifest: %s", text)
	}
	if strings.Contains(text, `"started_at"`) || strings.Contains(text, `"completed_at"`) {
		t.Fatalf("execution timestamps should not be present in manifest: %s", text)
	}
}

func TestBuildDiagnosticConfigTypesForTarget(t *testing.T) {
	tests := []struct {
		name string
		mode cluster.DeploymentMode
		role string
		want []appconfig.ConfigType
	}{
		{
			name: "hybrid master-worker",
			mode: cluster.DeploymentModeHybrid,
			role: string(cluster.NodeRoleMasterWorker),
			want: []appconfig.ConfigType{
				appconfig.ConfigTypeSeatunnel,
				appconfig.ConfigTypeHazelcast,
				appconfig.ConfigTypeHazelcastClient,
			},
		},
		{
			name: "separated master",
			mode: cluster.DeploymentModeSeparated,
			role: string(cluster.NodeRoleMaster),
			want: []appconfig.ConfigType{
				appconfig.ConfigTypeSeatunnel,
				appconfig.ConfigTypeHazelcastMaster,
				appconfig.ConfigTypeHazelcastClient,
			},
		},
		{
			name: "separated worker",
			mode: cluster.DeploymentModeSeparated,
			role: string(cluster.NodeRoleWorker),
			want: []appconfig.ConfigType{
				appconfig.ConfigTypeSeatunnel,
				appconfig.ConfigTypeHazelcastWorker,
				appconfig.ConfigTypeHazelcastClient,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDiagnosticConfigTypesForTarget(tt.mode, tt.role)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d config types, got %d: %v", len(tt.want), len(got), got)
			}
			for index, item := range tt.want {
				if got[index] != item {
					t.Fatalf("expected config type %q at index %d, got %q", item, index, got[index])
				}
			}
		})
	}
}

func TestDetectDiagnosticConfigFormat(t *testing.T) {
	if got := detectDiagnosticConfigFormat("seatunnel.yaml"); got != "yaml" {
		t.Fatalf("expected yaml, got %s", got)
	}
	if got := detectDiagnosticConfigFormat("log4j2.properties"); got != "properties" {
		t.Fatalf("expected properties, got %s", got)
	}
	if got := detectDiagnosticConfigFormat("jvm_options"); got != "text" {
		t.Fatalf("expected text, got %s", got)
	}
}

func TestExtractDiagnosticConfigHighlights_extractsYAMLAndJVMSettings(t *testing.T) {
	yamlContent := `
metrics:
  enabled: true
  prometheus:
    enabled: true
seatunnel:
  engine:
    backup-count: 2
    checkpoint:
      interval: 10000
`
	yamlHighlights := extractDiagnosticConfigHighlights("seatunnel.yaml", "/opt/seatunnel/config/seatunnel.yaml", yamlContent)
	if !containsConfigHighlight(yamlHighlights, "Metrics", "true") {
		t.Fatalf("expected metrics highlight, got %#v", yamlHighlights)
	}
	if !containsConfigHighlight(yamlHighlights, "Prometheus", "true") {
		t.Fatalf("expected prometheus highlight, got %#v", yamlHighlights)
	}
	if !containsConfigHighlight(yamlHighlights, "Backup Count", "2") {
		t.Fatalf("expected backup-count highlight, got %#v", yamlHighlights)
	}

	jvmContent := `
# comment
-Xms2g
-Xmx4g
-XX:+HeapDumpOnOutOfMemoryError
-XX:HeapDumpPath=/tmp/heap.hprof
`
	jvmHighlights := extractDiagnosticConfigHighlights("jvm_options", "/opt/seatunnel/config/jvm_options", jvmContent)
	if !containsConfigHighlight(jvmHighlights, "Xms", "2g") {
		t.Fatalf("expected Xms highlight, got %#v", jvmHighlights)
	}
	if !containsConfigHighlight(jvmHighlights, "Xmx", "4g") {
		t.Fatalf("expected Xmx highlight, got %#v", jvmHighlights)
	}
	if !containsConfigHighlight(jvmHighlights, "OOM HeapDump", "true") {
		t.Fatalf("expected heap dump highlight, got %#v", jvmHighlights)
	}
}

func TestBuildDiagnosticExtraConfigFilesForTarget(t *testing.T) {
	got := buildDiagnosticExtraConfigFilesForTarget(cluster.DeploymentModeSeparated, string(cluster.NodeRoleMaster))
	expected := []string{"log4j2.properties", "log4j2_client.properties", "plugin_config", "jvm_client_options", "jvm_master_options"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d extra files, got %d: %v", len(expected), len(got), got)
	}
	for index, item := range expected {
		if got[index] != item {
			t.Fatalf("expected %q at index %d, got %q", item, index, got[index])
		}
	}
}

func TestBuildDiagnosticPrometheusSignalSpecs_includeGCOldGen(t *testing.T) {
	specs := buildDiagnosticPrometheusSignalSpecs(6)
	keys := make(map[string]diagnosticPrometheusSignalSpec, len(specs))
	for _, spec := range specs {
		keys[spec.Key] = spec
	}

	oldGen, ok := keys["old_gen_usage_high"]
	if !ok {
		t.Fatalf("expected old_gen_usage_high signal spec")
	}
	if !strings.Contains(oldGen.PromQL, "jvm_memory_pool_bytes_used") || !strings.Contains(oldGen.PromQL, "Old Gen") {
		t.Fatalf("unexpected old-gen promql: %s", oldGen.PromQL)
	}

	gc, ok := keys["gc_time_ratio_high"]
	if !ok {
		t.Fatalf("expected gc_time_ratio_high signal spec")
	}
	if gc.Unit != "percent" {
		t.Fatalf("expected percent unit for gc signal, got %s", gc.Unit)
	}
	if !strings.Contains(gc.PromQL, "jvm_gc_collection_seconds_sum") {
		t.Fatalf("unexpected gc promql: %s", gc.PromQL)
	}
}

func TestBuildDiagnosticBundleHTMLMetricsPanel_prioritizesAnomalies(t *testing.T) {
	panel := buildDiagnosticBundleHTMLMetricsPanel(&diagnosticPrometheusSnapshot{
		Signals: []diagnosticPrometheusSignal{
			{Title: "CPU", Status: "warning"},
			{Title: "Heap", Status: "critical"},
			{Title: "FD", Status: "healthy"},
		},
	})
	if panel == nil {
		t.Fatal("expected metrics panel")
	}
	if panel.AnomalyCount != 2 {
		t.Fatalf("expected anomaly count 2, got %d", panel.AnomalyCount)
	}
	if len(panel.HighlightedSignals) != 2 {
		t.Fatalf("expected 2 highlighted signals, got %d", len(panel.HighlightedSignals))
	}
	if len(panel.AdditionalSignals) != 1 {
		t.Fatalf("expected 1 additional signal, got %d", len(panel.AdditionalSignals))
	}
}

func TestExecuteCollectProcessEventsStep_fallsBackWhenFilteredQueryReturnsEmpty(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(&DiagnosticTask{}, &DiagnosticTaskStep{}, &DiagnosticStepLog{}); err != nil {
		t.Fatalf("auto migrate diagnostics task models: %v", err)
	}

	now := time.Now().UTC()
	reader := &fakeDiagnosticProcessEventReader{
		filteredRows: []*monitor.ProcessEventWithHost{},
		clusterEvents: []*monitor.ProcessEvent{
			{
				ID:          1,
				ClusterID:   6,
				NodeID:      4,
				HostID:      4,
				EventType:   monitor.EventTypeNodeOffline,
				ProcessName: "seatunnel",
				CreatedAt:   now.Add(-10 * time.Minute),
			},
			{
				ID:          2,
				ClusterID:   6,
				NodeID:      4,
				HostID:      4,
				EventType:   monitor.EventTypeNodeRecovered,
				ProcessName: "seatunnel",
				CreatedAt:   now.Add(-5 * time.Minute),
			},
		},
	}

	service := NewServiceWithRepository(NewRepository(database), nil, reader, nil)
	task := &DiagnosticTask{
		ID:              1,
		ClusterID:       6,
		LookbackMinutes: 60,
	}
	step := &DiagnosticTaskStep{
		ID:   1,
		Code: DiagnosticStepCodeCollectProcessEvents,
	}
	state := &diagnosticBundleExecutionState{}
	bundleDir := t.TempDir()

	if err := service.executeCollectProcessEventsStep(t.Context(), task, step, state, bundleDir); err != nil {
		t.Fatalf("executeCollectProcessEventsStep returned error: %v", err)
	}
	if reader.listEventsCalls == 0 {
		t.Fatal("expected filtered ListEvents to be called")
	}
	if reader.clusterEventCalls == 0 {
		t.Fatal("expected fallback ListClusterEvents to be called")
	}
	if len(state.ProcessEvents) != 2 {
		t.Fatalf("expected 2 process events after fallback, got %d", len(state.ProcessEvents))
	}
	payload, err := os.ReadFile(bundleDir + "/process-events.json")
	if err != nil {
		t.Fatalf("read process-events artifact: %v", err)
	}
	if !strings.Contains(string(payload), "node_offline") || !strings.Contains(string(payload), "node_recovered") {
		t.Fatalf("expected process-events artifact to contain fallback events, got %s", string(payload))
	}
}

func TestQueryDiagnosticPrometheusSignal_marksWarningWhenThresholdBreached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"instance":"node-1"},"values":[[1,"0.91"],[2,"0.95"],[3,"0.93"]]}]}}`))
	}))
	defer server.Close()

	signal, err := queryDiagnosticPrometheusSignal(
		t.Context(),
		server.URL,
		diagnosticCollectionWindow{
			Start: time.Unix(0, 0).UTC(),
			End:   time.Unix(180, 0).UTC(),
		},
		60,
		diagnosticPrometheusSignalSpec{
			Key:            "cpu_usage_high",
			Title:          "CPU",
			Unit:           "cores",
			Threshold:      0.8,
			ThresholdText:  "> 0.8 cores",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL:         "test_cpu_metric",
		},
	)
	if err != nil {
		t.Fatalf("queryDiagnosticPrometheusSignal returned error: %v", err)
	}
	if signal.Status != "warning" {
		t.Fatalf("expected warning status, got %s", signal.Status)
	}
	if len(signal.Series) != 1 || signal.Series[0].MaxValue <= 0.8 {
		t.Fatalf("expected breached series summary, got %+v", signal.Series)
	}
}

func TestDiagnosticBundleHTMLTemplateParsesAndRendersMetricsPanels(t *testing.T) {
	tmpl, err := template.New("diagnostic-summary").Funcs(template.FuncMap{
		"formatTime": func(value interface{}) string {
			switch typed := value.(type) {
			case *time.Time:
				return formatDiagnosticBundleTime(typed)
			case time.Time:
				return formatDiagnosticBundleTimeValue(typed)
			default:
				return "-"
			}
		},
		"statusClass": func(status interface{}) string {
			return diagnosticHTMLStatusClass(fmt.Sprint(status))
		},
		"toneClass": func(tone interface{}) string {
			return diagnosticHTMLToneClass(fmt.Sprint(tone))
		},
		"formatBytes": func(size int64) string {
			return formatDiagnosticBytes(size)
		},
		"formatMetricValue": func(unit string, value float64) string {
			return formatDiagnosticMetricValue(unit, value)
		},
		"shortHash": func(value string) string {
			value = strings.TrimSpace(value)
			if len(value) <= 12 {
				return value
			}
			return value[:12]
		},
	}).Parse(diagnosticBundleHTMLTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	now := time.Now().UTC()
	payload := &diagnosticBundleHTMLPayload{
		GeneratedAt: now,
		Health: diagnosticBundleHTMLHealthSummary{
			Tone:    "warning",
			Title:   "warning",
			Summary: "summary",
			Metrics: []diagnosticBundleHTMLMetricCard{{Label: "time", Value: "30m"}},
		},
		Task: diagnosticBundleHTMLTaskSummary{
			ID:        1,
			Status:    DiagnosticTaskStatusSucceeded,
			Summary:   "task",
			CreatedBy: "tester",
		},
		MetricsSnapshot: &diagnosticBundleHTMLMetricsPanel{
			SignalCount:  1,
			AnomalyCount: 1,
			HighlightedSignals: []diagnosticPrometheusSignal{{
				Title:         "CPU",
				ThresholdText: "> 0.8",
				Status:        "warning",
				Unit:          "ratio",
				Summary:       "cpu summary",
				Series: []diagnosticPrometheusSeriesSummary{{
					Instance:  "node-1",
					MinValue:  0.2,
					MaxValue:  0.9,
					LastValue: 0.7,
					Samples:   5,
				}},
			}},
			CollectionNotes: []string{"partial query failed"},
		},
		ConfigSnapshot: &diagnosticBundleHTMLConfigPanel{
			FileCount:          1,
			KeyHighlightCount:  1,
			DirectoryCount:     1,
			ChangedConfigCount: 1,
			KeyHighlights: []diagnosticConfigKeyHighlight{{
				HostID:     1,
				Role:       "master",
				ConfigType: "seatunnel.yaml",
				RemotePath: "/opt/seatunnel/config/seatunnel.yaml",
				Items: []diagnosticConfigKeyValue{{
					Label: "Metrics",
					Value: "true",
				}},
			}},
			RecentChanges: []diagnosticConfigChangeRecord{{
				ConfigType: "seatunnel.yaml",
				HostScope:  "template",
				Version:    2,
				FilePath:   "config/seatunnel.yaml",
				UpdatedAt:  now,
			}},
			Files: []diagnosticConfigSnapshotFile{{
				HostID:      1,
				Role:        "master",
				ConfigType:  "seatunnel.yaml",
				RemotePath:  "/opt/seatunnel/config/seatunnel.yaml",
				SizeBytes:   1024,
				ContentHash: "1234567890abcdef",
			}},
			ConfigChanges: []diagnosticConfigChangeRecord{{
				ConfigType: "seatunnel.yaml",
				HostScope:  "template",
				Version:    2,
				FilePath:   "config/seatunnel.yaml",
				UpdatedAt:  now,
			}},
			DirectoryManifests: []diagnosticDirectoryManifest{{
				Directory:  "/tmp/connectors",
				EntryCount: 1,
				Entries: []diagnosticDirectoryManifestItem{{
					Name:    "connector-fake.jar",
					Path:    "/tmp/connectors/connector-fake.jar",
					Size:    123,
					ModTime: now,
				}},
			}},
			CollectionNotes: []diagnosticConfigSnapshotNote{{
				HostID:     1,
				Role:       "master",
				ConfigType: "jvm_master_options",
				Message:    "file missing",
			}},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	if !strings.Contains(buf.String(), "Prometheus") {
		t.Fatalf("expected rendered html to contain Prometheus panel")
	}
	if !strings.Contains(buf.String(), "Key Runtime Settings") || !strings.Contains(buf.String(), "connector-fake.jar") {
		t.Fatalf("expected rendered html to contain config inventory details")
	}
}

func TestFormatDiagnosticMetricValue_percent(t *testing.T) {
	if got := formatDiagnosticMetricValue("percent", 12.345); got != "12.3%" {
		t.Fatalf("expected percent metric formatting, got %s", got)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsConfigHighlight(items []diagnosticConfigKeyValue, label, value string) bool {
	for _, item := range items {
		if strings.Contains(item.Label, label) && item.Value == value {
			return true
		}
	}
	return false
}

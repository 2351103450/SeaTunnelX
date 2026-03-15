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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seatunnel/seatunnelX/internal/apps/cluster"
	appconfig "github.com/seatunnel/seatunnelX/internal/apps/config"
	"github.com/seatunnel/seatunnelX/internal/apps/monitor"
	monitoringapp "github.com/seatunnel/seatunnelX/internal/apps/monitoring"
	"github.com/seatunnel/seatunnelX/internal/config"
	"github.com/seatunnel/seatunnelX/internal/logger"
	"gopkg.in/yaml.v3"
)

var (
	diagnosticDisplayTimezoneOnce sync.Once
	diagnosticDisplayTimezone     *time.Location
)

type diagnosticBundleArtifact struct {
	StepCode   DiagnosticStepCode `json:"step_code"`
	Category   string             `json:"category"`
	Format     string             `json:"format"`
	Status     string             `json:"status"`
	Path       string             `json:"path,omitempty"`
	RemotePath string             `json:"remote_path,omitempty"`
	NodeID     uint               `json:"node_id,omitempty"`
	HostID     uint               `json:"host_id,omitempty"`
	HostName   string             `json:"host_name,omitempty"`
	SizeBytes  int64              `json:"size_bytes,omitempty"`
	Message    string             `json:"message,omitempty"`
}

type diagnosticBundleManifest struct {
	Version         string                      `json:"version"`
	TaskID          uint                        `json:"task_id"`
	ClusterID       uint                        `json:"cluster_id"`
	TriggerSource   DiagnosticTaskSourceType    `json:"trigger_source"`
	SourceRef       DiagnosticTaskSourceRef     `json:"source_ref"`
	Options         DiagnosticTaskOptions       `json:"options"`
	Status          DiagnosticTaskStatus        `json:"status"`
	Summary         string                      `json:"summary"`
	LookbackMinutes int                         `json:"lookback_minutes,omitempty"`
	WindowStart     *time.Time                  `json:"window_start,omitempty"`
	WindowEnd       *time.Time                  `json:"window_end,omitempty"`
	GeneratedAt     time.Time                   `json:"generated_at"`
	Artifacts       []*diagnosticBundleArtifact `json:"artifacts"`
}

type diagnosticBundleExecutionState struct {
	WindowStart      *time.Time
	WindowEnd        *time.Time
	LookbackMinutes  int
	ErrorGroup       *SeatunnelErrorGroup
	ErrorEvents      []*SeatunnelErrorEvent
	LogSamples       []diagnosticCollectedLogSample
	InspectionDetail *ClusterInspectionReportDetailData
	ProcessEvents    []*monitor.ProcessEvent
	AlertSnapshot    []*monitoringapp.AlertInstance
	ClusterSnapshot  *cluster.Cluster
	ConfigSnapshot   *diagnosticConfigSnapshotSummary
	MetricsSnapshot  *diagnosticPrometheusSnapshot
	Artifacts        []*diagnosticBundleArtifact
}

type diagnosticCollectionWindow struct {
	Start           time.Time
	End             time.Time
	LookbackMinutes int
}

type diagnosticCollectionWindowPayload struct {
	StartAt         time.Time `json:"start_at"`
	EndAt           time.Time `json:"end_at"`
	LookbackMinutes int       `json:"lookback_minutes"`
}

type diagnosticConfigSnapshotSummary struct {
	ClusterID          uint                           `json:"cluster_id"`
	ClusterName        string                         `json:"cluster_name,omitempty"`
	DeploymentMode     string                         `json:"deployment_mode,omitempty"`
	CollectedAt        time.Time                      `json:"collected_at"`
	Files              []diagnosticConfigSnapshotFile `json:"files"`
	KeyHighlights      []diagnosticConfigKeyHighlight `json:"key_highlights,omitempty"`
	FilePreviews       []diagnosticConfigFilePreview  `json:"file_previews,omitempty"`
	DirectoryManifests []diagnosticDirectoryManifest  `json:"directory_manifests,omitempty"`
	ConfigChanges      []diagnosticConfigChangeRecord `json:"config_changes,omitempty"`
	CollectionNotes    []diagnosticConfigSnapshotNote `json:"collection_notes,omitempty"`
}

type diagnosticConfigSnapshotFile struct {
	HostID      uint   `json:"host_id"`
	HostName    string `json:"host_name,omitempty"`
	HostIP      string `json:"host_ip,omitempty"`
	NodeID      uint   `json:"node_id"`
	Role        string `json:"role,omitempty"`
	InstallDir  string `json:"install_dir,omitempty"`
	ConfigType  string `json:"config_type"`
	RemotePath  string `json:"remote_path,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

type diagnosticConfigSnapshotNote struct {
	HostID     uint   `json:"host_id"`
	Role       string `json:"role,omitempty"`
	ConfigType string `json:"config_type,omitempty"`
	Message    string `json:"message"`
}

type diagnosticConfigKeyHighlight struct {
	HostID     uint                       `json:"host_id"`
	HostName   string                     `json:"host_name,omitempty"`
	HostIP     string                     `json:"host_ip,omitempty"`
	NodeID     uint                       `json:"node_id"`
	Role       string                     `json:"role,omitempty"`
	ConfigType string                     `json:"config_type"`
	RemotePath string                     `json:"remote_path,omitempty"`
	Items      []diagnosticConfigKeyValue `json:"items"`
}

type diagnosticConfigKeyValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type diagnosticConfigFilePreview struct {
	HostID     uint   `json:"host_id"`
	HostName   string `json:"host_name,omitempty"`
	HostIP     string `json:"host_ip,omitempty"`
	NodeID     uint   `json:"node_id"`
	Role       string `json:"role,omitempty"`
	ConfigType string `json:"config_type"`
	RemotePath string `json:"remote_path,omitempty"`
	Preview    string `json:"preview"`
}

type diagnosticDirectoryManifest struct {
	HostID     uint                              `json:"host_id"`
	HostName   string                            `json:"host_name,omitempty"`
	HostIP     string                            `json:"host_ip,omitempty"`
	NodeID     uint                              `json:"node_id"`
	Role       string                            `json:"role,omitempty"`
	Directory  string                            `json:"directory"`
	LocalPath  string                            `json:"local_path,omitempty"`
	EntryCount int                               `json:"entry_count"`
	Entries    []diagnosticDirectoryManifestItem `json:"entries"`
}

type diagnosticDirectoryManifestItem struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode,omitempty"`
	ModTime time.Time `json:"mod_time,omitempty"`
	IsDir   bool      `json:"is_dir"`
}

type diagnosticConfigChangeRecord struct {
	ConfigID   uint      `json:"config_id"`
	ClusterID  uint      `json:"cluster_id"`
	HostID     *uint     `json:"host_id,omitempty"`
	HostScope  string    `json:"host_scope,omitempty"`
	ConfigType string    `json:"config_type"`
	FilePath   string    `json:"file_path,omitempty"`
	Version    int       `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  uint      `json:"updated_by"`
	IsTemplate bool      `json:"is_template"`
}

type diagnosticCollectedLogSample struct {
	HostID      uint      `json:"host_id"`
	HostName    string    `json:"host_name,omitempty"`
	HostIP      string    `json:"host_ip,omitempty"`
	NodeID      uint      `json:"node_id"`
	Role        string    `json:"role,omitempty"`
	SourceFile  string    `json:"source_file"`
	LocalPath   string    `json:"local_path,omitempty"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Content     string    `json:"content"`
}

type agentPullConfigResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	ConfigType string `json:"config_type"`
	Content    string `json:"content"`
	FilePath   string `json:"file_path"`
}

type agentDirectoryListingResult struct {
	Path    string                            `json:"path"`
	Entries []diagnosticDirectoryManifestItem `json:"entries"`
}

type diagnosticPrometheusSnapshot struct {
	ClusterID       uint                         `json:"cluster_id"`
	WindowStart     time.Time                    `json:"window_start"`
	WindowEnd       time.Time                    `json:"window_end"`
	StepSeconds     int                          `json:"step_seconds"`
	Signals         []diagnosticPrometheusSignal `json:"signals"`
	CollectionNotes []string                     `json:"collection_notes,omitempty"`
}

type diagnosticPrometheusSignal struct {
	Key           string                              `json:"key"`
	Title         string                              `json:"title"`
	Summary       string                              `json:"summary"`
	PromQL        string                              `json:"promql"`
	Unit          string                              `json:"unit"`
	Threshold     float64                             `json:"threshold"`
	ThresholdText string                              `json:"threshold_text"`
	Status        string                              `json:"status"`
	Comparator    string                              `json:"comparator"`
	Series        []diagnosticPrometheusSeriesSummary `json:"series"`
}

type diagnosticPrometheusPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type diagnosticPrometheusSeriesSummary struct {
	Instance  string                      `json:"instance"`
	MinValue  float64                     `json:"min_value"`
	MinAt     *time.Time                  `json:"min_at,omitempty"`
	MaxValue  float64                     `json:"max_value"`
	MaxAt     *time.Time                  `json:"max_at,omitempty"`
	LastValue float64                     `json:"last_value"`
	LastAt    *time.Time                  `json:"last_at,omitempty"`
	Samples   int                         `json:"samples"`
	Points    []diagnosticPrometheusPoint `json:"points,omitempty"`
}

type diagnosticBundleHTMLPayload struct {
	GeneratedAt time.Time `json:"generated_at"`
	// Summary：聚焦一句话结论 + 少量关键指标
	Health diagnosticBundleHTMLHealthSummary `json:"health"`
	// Critical Findings：按严重级别排序的关键发现（来自巡检发现或错误/告警上下文）
	Inspection *diagnosticBundleHTMLInspectionPanel `json:"inspection,omitempty"`
	// Evidence：证据详情，按需展开
	ErrorContext    *diagnosticBundleHTMLErrorPanel     `json:"error_context,omitempty"`
	AlertSnapshot   *diagnosticBundleHTMLAlertPanel     `json:"alert_snapshot,omitempty"`
	ProcessEvents   *diagnosticBundleHTMLProcessPanel   `json:"process_events,omitempty"`
	ConfigSnapshot  *diagnosticBundleHTMLConfigPanel    `json:"config_snapshot,omitempty"`
	MetricsSnapshot *diagnosticBundleHTMLMetricsPanel   `json:"metrics_snapshot,omitempty"`
	ArtifactGroups  []diagnosticBundleHTMLArtifactGroup `json:"artifact_groups"`

	// 附录类信息：任务概览、执行过程、溯源与建议，弱化展示
	Cluster            *diagnosticBundleHTMLClusterSummary `json:"cluster,omitempty"`
	Task               diagnosticBundleHTMLTaskSummary     `json:"task"`
	TaskExecution      diagnosticBundleHTMLExecutionPanel  `json:"task_execution"`
	SourceTraceability []diagnosticBundleHTMLTraceItem     `json:"source_traceability"`
	Recommendations    []diagnosticBundleHTMLAdvice        `json:"recommendations"`
	PassedChecks       []diagnosticBundleHTMLAdvice        `json:"passed_checks"`
}

type diagnosticBundleHTMLHealthSummary struct {
	Tone    string                           `json:"tone"`
	Title   string                           `json:"title"`
	Summary string                           `json:"summary"`
	Metrics []diagnosticBundleHTMLMetricCard `json:"metrics"`
}

type diagnosticBundleHTMLTaskSummary struct {
	ID            uint                             `json:"id"`
	Status        DiagnosticTaskStatus             `json:"status"`
	TriggerSource DiagnosticTaskSourceType         `json:"trigger_source"`
	Summary       string                           `json:"summary"`
	CreatedBy     string                           `json:"created_by"`
	StartedAt     *time.Time                       `json:"started_at,omitempty"`
	CompletedAt   *time.Time                       `json:"completed_at,omitempty"`
	BundleDir     string                           `json:"bundle_dir"`
	ManifestPath  string                           `json:"manifest_path"`
	IndexPath     string                           `json:"index_path"`
	Options       DiagnosticTaskOptions            `json:"options"`
	SelectedNodes []diagnosticBundleHTMLNodeTarget `json:"selected_nodes"`
}

type diagnosticBundleHTMLClusterSummary struct {
	ID             uint                              `json:"id"`
	Name           string                            `json:"name"`
	Version        string                            `json:"version"`
	Status         string                            `json:"status"`
	DeploymentMode string                            `json:"deployment_mode"`
	InstallDir     string                            `json:"install_dir"`
	NodeCount      int                               `json:"node_count"`
	Nodes          []diagnosticBundleHTMLClusterNode `json:"nodes"`
}

type diagnosticBundleHTMLClusterNode struct {
	Role       string `json:"role"`
	HostID     uint   `json:"host_id"`
	InstallDir string `json:"install_dir"`
	Status     string `json:"status"`
	ProcessPID int    `json:"process_pid"`
}

type diagnosticBundleHTMLInspectionPanel struct {
	Summary         string                        `json:"summary"`
	Status          InspectionReportStatus        `json:"status"`
	RequestedBy     string                        `json:"requested_by"`
	LookbackMinutes int                           `json:"lookback_minutes"`
	CriticalCount   int                           `json:"critical_count"`
	WarningCount    int                           `json:"warning_count"`
	InfoCount       int                           `json:"info_count"`
	StartedAt       *time.Time                    `json:"started_at,omitempty"`
	FinishedAt      *time.Time                    `json:"finished_at,omitempty"`
	Findings        []diagnosticBundleHTMLFinding `json:"findings"`
}

type diagnosticBundleHTMLFinding struct {
	Severity       string `json:"severity"`
	CheckName      string `json:"check_name"`
	CheckCode      string `json:"check_code"`
	Summary        string `json:"summary"`
	Recommendation string `json:"recommendation"`
	Evidence       string `json:"evidence"`
}

type diagnosticBundleHTMLErrorPanel struct {
	GroupTitle       string                           `json:"group_title"`
	ExceptionClass   string                           `json:"exception_class"`
	OccurrenceCount  int64                            `json:"occurrence_count"`
	FirstSeenAt      *time.Time                       `json:"first_seen_at,omitempty"`
	LastSeenAt       *time.Time                       `json:"last_seen_at,omitempty"`
	SampleMessage    string                           `json:"sample_message"`
	RecentEventCount int                              `json:"recent_event_count"`
	Events           []diagnosticBundleHTMLErrorEvent `json:"events"`
	LogSamples       []diagnosticBundleHTMLLogSample  `json:"log_samples"`
}

type diagnosticBundleHTMLErrorEvent struct {
	OccurredAt string `json:"occurred_at"`
	Role       string `json:"role"`
	HostLabel  string `json:"host_label"`
	SourceFile string `json:"source_file"`
	JobID      string `json:"job_id"`
	Message    string `json:"message"`
	Evidence   string `json:"evidence"`
}

type diagnosticBundleHTMLAlertPanel struct {
	Total       int                             `json:"total"`
	Critical    int                             `json:"critical"`
	Warning     int                             `json:"warning"`
	Firing      int                             `json:"firing"`
	FirstSeenAt string                          `json:"first_seen_at"`
	LastSeenAt  string                          `json:"last_seen_at"`
	Alerts      []diagnosticBundleHTMLAlertItem `json:"alerts"`
}

type diagnosticBundleHTMLAlertItem struct {
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	FiringAt    string `json:"firing_at"`
	LastSeenAt  string `json:"last_seen_at"`
	ResolvedAt  string `json:"resolved_at"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

type diagnosticBundleHTMLProcessPanel struct {
	Total  int                                `json:"total"`
	ByType []diagnosticBundleHTMLMetricCard   `json:"by_type"`
	Events []diagnosticBundleHTMLProcessEvent `json:"events"`
}

type diagnosticBundleHTMLConfigPanel struct {
	FileCount          int                            `json:"file_count"`
	KeyHighlightCount  int                            `json:"key_highlight_count"`
	DirectoryCount     int                            `json:"directory_count"`
	ChangedConfigCount int                            `json:"changed_config_count"`
	KeyHighlights      []diagnosticConfigKeyHighlight `json:"key_highlights"`
	FilePreviews       []diagnosticConfigFilePreview  `json:"file_previews"`
	RecentChanges      []diagnosticConfigChangeRecord `json:"recent_changes"`
	RemainingChanges   []diagnosticConfigChangeRecord `json:"remaining_changes"`
	Files              []diagnosticConfigSnapshotFile `json:"files"`
	DirectoryManifests []diagnosticDirectoryManifest  `json:"directory_manifests"`
	ConfigChanges      []diagnosticConfigChangeRecord `json:"config_changes"`
	CollectionNotes    []diagnosticConfigSnapshotNote `json:"collection_notes"`
}

type diagnosticBundleHTMLMetricsPanel struct {
	SignalCount        int                          `json:"signal_count"`
	AnomalyCount       int                          `json:"anomaly_count"`
	HighlightedSignals []diagnosticPrometheusSignal `json:"highlighted_signals"`
	AdditionalSignals  []diagnosticPrometheusSignal `json:"additional_signals"`
	CollectionNotes    []string                     `json:"collection_notes,omitempty"`
}

type diagnosticBundleHTMLProcessEvent struct {
	CreatedAt   string `json:"created_at"`
	EventType   string `json:"event_type"`
	ProcessName string `json:"process_name"`
	NodeLabel   string `json:"node_label"`
	Details     string `json:"details"`
}

type diagnosticBundleHTMLLogSample struct {
	HostLabel   string `json:"host_label"`
	SourceFile  string `json:"source_file"`
	WindowLabel string `json:"window_label"`
	Content     string `json:"content"`
}

type diagnosticBundleHTMLExecutionPanel struct {
	Steps []diagnosticBundleHTMLExecutionStep `json:"steps"`
	Nodes []diagnosticBundleHTMLExecutionNode `json:"nodes"`
}

type diagnosticBundleHTMLExecutionStep struct {
	Sequence    int    `json:"sequence"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Error       string `json:"error"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type diagnosticBundleHTMLExecutionNode struct {
	HostLabel   string `json:"host_label"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	CurrentStep string `json:"current_step"`
	Message     string `json:"message"`
	Error       string `json:"error"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type diagnosticBundleHTMLArtifactGroup struct {
	Key   string                             `json:"key"`
	Label string                             `json:"label"`
	Items []diagnosticBundleHTMLArtifactView `json:"items"`
}

type diagnosticBundleHTMLArtifactView struct {
	Category      string `json:"category"`
	CategoryLabel string `json:"category_label"`
	StepCode      string `json:"step_code"`
	Status        string `json:"status"`
	Format        string `json:"format"`
	HostLabel     string `json:"host_label"`
	LocalPath     string `json:"local_path"`
	RelativePath  string `json:"relative_path"`
	RemotePath    string `json:"remote_path"`
	SizeLabel     string `json:"size_label"`
	Message       string `json:"message"`
	Preview       string `json:"preview"`
	PreviewNote   string `json:"preview_note"`
}

type diagnosticBundleHTMLNodeTarget struct {
	HostLabel   string `json:"host_label"`
	Role        string `json:"role"`
	InstallDir  string `json:"install_dir"`
	ClusterNode string `json:"cluster_node"`
}

type diagnosticBundleHTMLTraceItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type diagnosticBundleHTMLMetricCard struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

type diagnosticBundleHTMLAdvice struct {
	Title   string `json:"title"`
	Details string `json:"details"`
}

type agentThreadDumpResult struct {
	Status      string    `json:"status"`
	PID         int       `json:"pid"`
	Role        string    `json:"role"`
	InstallDir  string    `json:"install_dir"`
	Tool        string    `json:"tool"`
	OutputPath  string    `json:"output_path"`
	SizeBytes   int64     `json:"size_bytes"`
	Message     string    `json:"message"`
	Content     string    `json:"content"`
	CollectedAt time.Time `json:"collected_at"`
}

type agentJVMDumpResult struct {
	Status         string    `json:"status"`
	PID            int       `json:"pid"`
	Role           string    `json:"role"`
	InstallDir     string    `json:"install_dir"`
	Tool           string    `json:"tool"`
	OutputPath     string    `json:"output_path"`
	SizeBytes      int64     `json:"size_bytes"`
	FreeBytes      int64     `json:"free_bytes"`
	RequiredBytes  int64     `json:"required_bytes"`
	EstimatedBytes int64     `json:"estimated_bytes"`
	Message        string    `json:"message"`
	CollectedAt    time.Time `json:"collected_at"`
}

func (s *Service) StartDiagnosticTask(ctx context.Context, taskID uint) error {
	if s == nil || s.repo == nil {
		return ErrDiagnosticsRepositoryUnavailable
	}
	task, err := s.repo.GetDiagnosticTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status == DiagnosticTaskStatusRunning {
		return nil
	}
	if task.Status == DiagnosticTaskStatusSucceeded {
		return nil
	}
	now := time.Now().UTC()
	if task.StartedAt == nil {
		task.StartedAt = &now
	}
	task.Status = DiagnosticTaskStatusRunning
	task.UpdatedAt = now
	if task.CurrentStep == "" {
		task.CurrentStep = resolveInitialDiagnosticTaskStep(DefaultDiagnosticTaskSteps(), task.Options.Normalize())
	}
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}
	go s.executeDiagnosticTask(context.Background(), task.ID)
	return nil
}

func (s *Service) executeDiagnosticTask(ctx context.Context, taskID uint) {
	task, err := s.repo.GetDiagnosticTaskByID(ctx, taskID)
	if err != nil {
		logger.ErrorF(ctx, "[DiagnosticsTask] load task failed: task_id=%d err=%v", taskID, err)
		return
	}
	if err := s.runDiagnosticTask(ctx, task); err != nil {
		logger.ErrorF(ctx, "[DiagnosticsTask] run task failed: task_id=%d err=%v", taskID, err)
	}
}

func (s *Service) runDiagnosticTask(ctx context.Context, task *DiagnosticTask) error {
	if task == nil {
		return ErrDiagnosticTaskNotFound
	}
	stepsByCode := make(map[DiagnosticStepCode]*DiagnosticTaskStep, len(task.Steps))
	for _, step := range task.Steps {
		stepsByCode[step.Code] = &DiagnosticTaskStep{
			ID:          step.ID,
			TaskID:      step.TaskID,
			Code:        step.Code,
			Sequence:    step.Sequence,
			Title:       step.Title,
			Description: step.Description,
			Status:      step.Status,
			Message:     step.Message,
			Error:       step.Error,
			StartedAt:   step.StartedAt,
			CompletedAt: step.CompletedAt,
			CreatedAt:   step.CreatedAt,
			UpdatedAt:   step.UpdatedAt,
		}
	}
	nodesByClusterNodeID := make(map[uint]*DiagnosticNodeExecution, len(task.NodeExecutions))
	for _, node := range task.NodeExecutions {
		copyNode := node
		nodesByClusterNodeID[node.ClusterNodeID] = &copyNode
	}

	state := &diagnosticBundleExecutionState{}
	bundleDir := diagnosticTaskBundleDir(task.ID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return s.failDiagnosticTask(ctx, task, DiagnosticStepCodeCollectConfigSnapshot, fmt.Errorf("create bundle dir: %w", err))
	}
	task.BundleDir = bundleDir
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}

	for _, planStep := range DefaultDiagnosticTaskSteps() {
		step := stepsByCode[planStep.Code]
		if step == nil {
			continue
		}
		if step.Status == DiagnosticTaskStatusSkipped {
			continue
		}

		if err := s.beginDiagnosticTaskStep(ctx, task, step); err != nil {
			return err
		}

		stepErr := s.executeDiagnosticPlanStep(ctx, task, step, planStep, nodesByClusterNodeID, state, bundleDir)
		if stepErr != nil {
			if err := s.failDiagnosticTaskStep(ctx, task, step, stepErr); err != nil {
				return err
			}
			if planStep.Required {
				return s.failDiagnosticTask(ctx, task, step.Code, stepErr)
			}
			_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
				TaskID:         task.ID,
				TaskStepID:     uintPtr(step.ID),
				StepCode:       step.Code,
				Level:          DiagnosticLogLevelWarn,
				EventType:      DiagnosticLogEventTypeNote,
				Message:        bilingualText("可选步骤失败，任务继续执行。", "Optional step failed and task will continue."),
				CreatedAt:      time.Now().UTC(),
				CommandSummary: stepErr.Error(),
			})
			continue
		}

		if err := s.finishDiagnosticTaskStep(ctx, task, step, bilingualText("步骤执行完成。", "Step completed.")); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	task.Status = DiagnosticTaskStatusSucceeded
	task.CurrentStep = DiagnosticStepCodeComplete
	task.CompletedAt = &now
	task.UpdatedAt = now
	task.Summary = strings.TrimSpace(task.Summary)
	if task.Summary == "" {
		task.Summary = bilingualText("诊断任务执行完成。", "Diagnostic bundle task completed.")
	}
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}
	if task.ManifestPath != "" {
		if err := writeDiagnosticBundleManifestFile(task.ManifestPath, task, state.Artifacts, state); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) executeDiagnosticPlanStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, planStep DiagnosticPlanStep, nodesByClusterNodeID map[uint]*DiagnosticNodeExecution, state *diagnosticBundleExecutionState, bundleDir string) error {
	switch step.Code {
	case DiagnosticStepCodeCollectErrorContext:
		return s.executeCollectErrorContextStep(ctx, task, step, state, bundleDir)
	case DiagnosticStepCodeCollectProcessEvents:
		return s.executeCollectProcessEventsStep(ctx, task, step, state, bundleDir)
	case DiagnosticStepCodeCollectAlertSnapshot:
		return s.executeCollectAlertSnapshotStep(ctx, task, step, state, bundleDir)
	case DiagnosticStepCodeCollectConfigSnapshot:
		return s.executeCollectConfigSnapshotStep(ctx, task, step, nodesByClusterNodeID, state, bundleDir)
	case DiagnosticStepCodeCollectLogSample:
		return s.executeCollectLogSampleStep(ctx, task, step, nodesByClusterNodeID, state, bundleDir)
	case DiagnosticStepCodeCollectThreadDump:
		return s.executeCollectThreadDumpStep(ctx, task, step, nodesByClusterNodeID, state, bundleDir)
	case DiagnosticStepCodeCollectJVMDump:
		return s.executeCollectJVMDumpStep(ctx, task, step, nodesByClusterNodeID, state, bundleDir)
	case DiagnosticStepCodeAssembleManifest:
		return s.executeAssembleManifestStep(ctx, task, step, state, bundleDir)
	case DiagnosticStepCodeRenderHTMLSummary:
		return s.executeRenderHTMLSummaryStep(ctx, task, step, state, bundleDir)
	case DiagnosticStepCodeComplete:
		return s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
			TaskID:     task.ID,
			TaskStepID: uintPtr(step.ID),
			StepCode:   step.Code,
			Level:      DiagnosticLogLevelInfo,
			EventType:  DiagnosticLogEventTypeSuccess,
			Message:    bilingualText("诊断任务执行完成。", "Diagnostic task completed."),
			CreatedAt:  time.Now().UTC(),
		})
	default:
		return nil
	}
}

func (s *Service) executeCollectErrorContextStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir string) error {
	payload := map[string]interface{}{
		"trigger_source": task.TriggerSource,
		"source_ref":     task.SourceRef,
	}
	if task.SourceRef.InspectionReportID > 0 {
		detail, err := s.GetInspectionReportDetail(ctx, task.SourceRef.InspectionReportID)
		if err != nil {
			return err
		}
		state.InspectionDetail = detail
		payload["inspection_detail"] = detail
	}
	window := resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
	state.WindowStart = timePtr(window.Start)
	state.WindowEnd = timePtr(window.End)
	state.LookbackMinutes = window.LookbackMinutes
	payload["collection_window"] = diagnosticCollectionWindowPayload{
		StartAt:         window.Start,
		EndAt:           window.End,
		LookbackMinutes: window.LookbackMinutes,
	}
	if task.SourceRef.ErrorGroupID > 0 {
		group, err := s.repo.GetErrorGroupByID(ctx, task.SourceRef.ErrorGroupID)
		if err != nil {
			return err
		}
		state.ErrorGroup = group
		events, _, err := s.repo.ListErrorEvents(ctx, &SeatunnelErrorEventFilter{
			ErrorGroupID: group.ID,
			StartTime:    timePtr(window.Start),
			EndTime:      timePtr(window.End),
			Page:         1,
			PageSize:     100,
		})
		if err != nil {
			return err
		}
		state.ErrorEvents = events
		payload["error_group"] = group
		payload["error_events"] = events
	}
	return s.writeDiagnosticJSONArtifact(ctx, task, step, state, bundleDir, "error-context.json", payload, &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "error_context",
		Format:   "json",
		Status:   "created",
	})
}

func (s *Service) executeCollectProcessEventsStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir string) error {
	window := resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
	state.WindowStart = timePtr(window.Start)
	state.WindowEnd = timePtr(window.End)
	state.LookbackMinutes = window.LookbackMinutes
	if s.monitorService == nil {
		state.ProcessEvents = []*monitor.ProcessEvent{}
	} else {
		loadedFromFilteredQuery := false
		if filteredReader, ok := s.monitorService.(interface {
			ListEvents(ctx context.Context, filter *monitor.ProcessEventFilter) ([]*monitor.ProcessEventWithHost, int64, error)
		}); ok {
			rows, _, err := filteredReader.ListEvents(ctx, &monitor.ProcessEventFilter{
				ClusterID: task.ClusterID,
				StartTime: timePtr(window.Start),
				EndTime:   timePtr(window.End),
				Page:      1,
				PageSize:  100,
			})
			if err != nil {
				return err
			}
			events := make([]*monitor.ProcessEvent, 0, len(rows))
			for _, row := range rows {
				if row == nil {
					continue
				}
				eventCopy := row.ProcessEvent
				events = append(events, &eventCopy)
			}
			state.ProcessEvents = filterDiagnosticProcessEventsByWindow(events, window.Start, window.End)
			loadedFromFilteredQuery = true
		}
		if !loadedFromFilteredQuery || len(state.ProcessEvents) == 0 {
			events, err := s.monitorService.ListClusterEvents(ctx, task.ClusterID, 200)
			if err != nil {
				return err
			}
			state.ProcessEvents = filterDiagnosticProcessEventsByWindow(events, window.Start, window.End)
		}
	}
	return s.writeDiagnosticJSONArtifact(ctx, task, step, state, bundleDir, "process-events.json", state.ProcessEvents, &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "process_events",
		Format:   "json",
		Status:   "created",
	})
}

func (s *Service) executeCollectAlertSnapshotStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir string) error {
	window := resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
	state.WindowStart = timePtr(window.Start)
	state.WindowEnd = timePtr(window.End)
	state.LookbackMinutes = window.LookbackMinutes
	alerts := make([]*monitoringapp.AlertInstance, 0)
	if s.monitoringService != nil {
		data, err := s.monitoringService.ListAlertInstances(ctx, &monitoringapp.AlertInstanceFilter{
			ClusterID: fmt.Sprintf("%d", task.ClusterID),
			Page:      1,
			PageSize:  200,
		})
		if err != nil {
			return err
		}
		if data != nil {
			alerts = data.Alerts
		}
	}
	state.AlertSnapshot = filterDiagnosticAlertsByWindow(alerts, window.Start, window.End, task.SourceRef.AlertID)
	if err := s.writeDiagnosticJSONArtifact(ctx, task, step, state, bundleDir, "alert-snapshot.json", state.AlertSnapshot, &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "alert_snapshot",
		Format:   "json",
		Status:   "created",
	}); err != nil {
		return err
	}
	metricsSnapshot, err := s.collectDiagnosticPrometheusSnapshot(ctx, task, window)
	if err != nil {
		_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
			TaskID:         task.ID,
			TaskStepID:     uintPtr(step.ID),
			StepCode:       step.Code,
			Level:          DiagnosticLogLevelWarn,
			EventType:      DiagnosticLogEventTypeNote,
			Message:        bilingualText(fmt.Sprintf("指标快照采集失败：%v", err), fmt.Sprintf("Metrics snapshot collection failed: %v", err)),
			CommandSummary: "metrics_snapshot",
			CreatedAt:      time.Now().UTC(),
		})
		return nil
	}
	if metricsSnapshot != nil && len(metricsSnapshot.Signals) > 0 {
		state.MetricsSnapshot = metricsSnapshot
		if err := s.writeDiagnosticJSONArtifact(ctx, task, step, state, bundleDir, "metrics-snapshot.json", metricsSnapshot, &diagnosticBundleArtifact{
			StepCode: step.Code,
			Category: "metrics_snapshot",
			Format:   "json",
			Status:   "created",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) executeCollectConfigSnapshotStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, nodesByClusterNodeID map[uint]*DiagnosticNodeExecution, state *diagnosticBundleExecutionState, bundleDir string) error {
	if s.clusterService == nil {
		return fmt.Errorf("cluster service is unavailable")
	}
	if s.agentSender == nil {
		return fmt.Errorf("agent sender is unavailable")
	}
	clusterInfo, err := s.clusterService.Get(ctx, task.ClusterID)
	if err != nil {
		return err
	}
	state.ClusterSnapshot = clusterInfo
	window := resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
	state.WindowStart = timePtr(window.Start)
	state.WindowEnd = timePtr(window.End)
	state.LookbackMinutes = window.LookbackMinutes
	configDir := filepath.Join(bundleDir, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	summary := &diagnosticConfigSnapshotSummary{
		ClusterID:          task.ClusterID,
		ClusterName:        strings.TrimSpace(clusterInfo.Name),
		DeploymentMode:     strings.TrimSpace(string(clusterInfo.DeploymentMode)),
		CollectedAt:        time.Now().UTC(),
		Files:              make([]diagnosticConfigSnapshotFile, 0, len(task.SelectedNodes)*6),
		KeyHighlights:      make([]diagnosticConfigKeyHighlight, 0, len(task.SelectedNodes)*4),
		FilePreviews:       make([]diagnosticConfigFilePreview, 0, len(task.SelectedNodes)*4),
		DirectoryManifests: make([]diagnosticDirectoryManifest, 0, len(task.SelectedNodes)*3),
		ConfigChanges:      make([]diagnosticConfigChangeRecord, 0),
		CollectionNotes:    make([]diagnosticConfigSnapshotNote, 0, len(task.SelectedNodes)),
	}
	var successCount int
	var errs []string
	for _, selected := range sortedDiagnosticTaskTargets(task.SelectedNodes) {
		node := nodesByClusterNodeID[selected.ClusterNodeID]
		if node == nil {
			continue
		}
		if err := s.beginDiagnosticNodeStep(ctx, step, node, bilingualText("正在采集 SeaTunnel 配置文件。", "Collecting SeaTunnel config files.")); err != nil {
			return err
		}
		seenPaths := make(map[string]struct{})
		configTypes := buildDiagnosticConfigTypesForTarget(clusterInfo.DeploymentMode, selected.Role)
		nodeSuccess := false
		for _, configType := range configTypes {
			result, detail, err := s.pullDiagnosticConfigFile(ctx, selected, configType)
			if err != nil || result == nil || !result.Success || strings.TrimSpace(result.Content) == "" {
				summary.CollectionNotes = append(summary.CollectionNotes, diagnosticConfigSnapshotNote{
					HostID:     selected.HostID,
					Role:       selected.Role,
					ConfigType: string(configType),
					Message:    detail,
				})
				_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
					TaskID:          task.ID,
					TaskStepID:      uintPtr(step.ID),
					NodeExecutionID: uintPtr(node.ID),
					StepCode:        step.Code,
					Level:           DiagnosticLogLevelWarn,
					EventType:       DiagnosticLogEventTypeNote,
					Message:         bilingualText(fmt.Sprintf("读取 %s 失败：%s", configType, detail), fmt.Sprintf("Failed to read %s: %s", configType, detail)),
					CommandSummary:  string(configType),
					CreatedAt:       time.Now().UTC(),
				})
				continue
			}
			if err := s.collectDiagnosticConfigArtifact(ctx, task, step, state, summary, configDir, selected, string(configType), firstNonEmptyString(result.FilePath, appconfig.GetConfigFilePath(configType), string(configType)), result.Content, node, seenPaths); err != nil {
				return err
			}
			nodeSuccess = true
		}
		for _, extraFile := range buildDiagnosticExtraConfigFilesForTarget(clusterInfo.DeploymentMode, selected.Role) {
			content, detail, err := s.pullDiagnosticRawFile(ctx, selected, filepath.Join(selected.InstallDir, "config", extraFile))
			if err != nil || strings.TrimSpace(content) == "" {
				summary.CollectionNotes = append(summary.CollectionNotes, diagnosticConfigSnapshotNote{
					HostID:     selected.HostID,
					Role:       selected.Role,
					ConfigType: extraFile,
					Message:    detail,
				})
				continue
			}
			if err := s.collectDiagnosticConfigArtifact(ctx, task, step, state, summary, configDir, selected, extraFile, filepath.Join(selected.InstallDir, "config", extraFile), content, node, seenPaths); err != nil {
				return err
			}
			nodeSuccess = true
		}
		for _, inventoryDir := range []string{"config", "lib", "connectors"} {
			manifest, detail, err := s.pullDiagnosticDirectoryManifest(ctx, selected, filepath.Join(selected.InstallDir, inventoryDir))
			if err != nil || manifest == nil {
				summary.CollectionNotes = append(summary.CollectionNotes, diagnosticConfigSnapshotNote{
					HostID:     selected.HostID,
					Role:       selected.Role,
					ConfigType: inventoryDir,
					Message:    detail,
				})
				continue
			}
			if err := s.collectDiagnosticDirectoryManifestArtifact(ctx, task, step, state, summary, configDir, selected, inventoryDir, manifest, node); err != nil {
				return err
			}
			nodeSuccess = true
		}
		if nodeSuccess {
			successCount++
			if err := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusSucceeded, bilingualText("配置文件采集完成。", "Config files collected.")); err != nil {
				return err
			}
			continue
		}
		errs = append(errs, fmt.Sprintf("host=%d", selected.HostID))
		if err := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusFailed, bilingualText("未采集到有效配置文件。", "No config files were collected.")); err != nil {
			return err
		}
	}
	if successCount == 0 {
		return formatDiagnosticAllNodesFailed("全部节点配置文件采集失败", "Config file collection failed on all nodes", errs)
	}
	if s.configRepo != nil {
		changes, err := s.configRepo.ListUpdatedByClusterBetween(ctx, task.ClusterID, window.Start, window.End)
		if err != nil {
			summary.CollectionNotes = append(summary.CollectionNotes, diagnosticConfigSnapshotNote{
				Message: fmt.Sprintf("load config change history failed: %v", err),
			})
		} else {
			summary.ConfigChanges = buildDiagnosticConfigChangeRecords(changes)
		}
	}
	state.ConfigSnapshot = summary
	return s.writeDiagnosticJSONArtifact(ctx, task, step, state, bundleDir, "config-snapshot.json", summary, &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "config_snapshot",
		Format:   "json",
		Status:   "created",
	})
}

func (s *Service) pullDiagnosticConfigFile(ctx context.Context, target DiagnosticTaskNodeTarget, configType appconfig.ConfigType) (*agentPullConfigResult, string, error) {
	success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, target.AgentID, "pull_config", map[string]string{
		"install_dir": target.InstallDir,
		"config_type": string(configType),
	})
	if err != nil || !success {
		detail := resolveDiagnosticCommandFailure(output, err, "配置文件采集失败。", "Config collection failed.")
		return nil, detail, err
	}
	var result agentPullConfigResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Sprintf("parse pull_config response failed: %v", err), err
	}
	if !result.Success {
		return &result, firstNonEmptyString(strings.TrimSpace(result.Message), "pull_config failed"), nil
	}
	return &result, "", nil
}

func (s *Service) pullDiagnosticRawFile(ctx context.Context, target DiagnosticTaskNodeTarget, absolutePath string) (string, string, error) {
	success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, target.AgentID, "get_logs", map[string]string{
		"log_file": absolutePath,
		"mode":     "all",
	})
	if err != nil || !success {
		detail := resolveDiagnosticCommandFailure(output, err, "文件采集失败。", "File collection failed.")
		return "", detail, err
	}
	return output, "", nil
}

func (s *Service) pullDiagnosticDirectoryManifest(ctx context.Context, target DiagnosticTaskNodeTarget, absolutePath string) (*agentDirectoryListingResult, string, error) {
	success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, target.AgentID, "get_logs", map[string]string{
		"log_file": absolutePath,
		"mode":     "list",
	})
	if err != nil || !success {
		detail := resolveDiagnosticCommandFailure(output, err, "目录清单采集失败。", "Directory manifest collection failed.")
		return nil, detail, err
	}
	var result agentDirectoryListingResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return nil, fmt.Sprintf("parse directory listing failed: %v", err), err
	}
	return &result, "", nil
}

func (s *Service) collectDiagnosticConfigArtifact(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, summary *diagnosticConfigSnapshotSummary, configDir string, target DiagnosticTaskNodeTarget, configType string, remotePath string, content string, node *DiagnosticNodeExecution, seenPaths map[string]struct{}) error {
	normalizedRemotePath := strings.TrimSpace(remotePath)
	if normalizedRemotePath == "" {
		normalizedRemotePath = strings.TrimSpace(configType)
	}
	if _, ok := seenPaths[normalizedRemotePath]; ok {
		return nil
	}
	seenPaths[normalizedRemotePath] = struct{}{}

	hostDir := filepath.Join(configDir, fmt.Sprintf("host-%d-%s", target.HostID, normalizeDiagnosticFileRole(target.Role)))
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return err
	}
	fileName := filepath.Base(normalizedRemotePath)
	localPath := filepath.Join(hostDir, fileName)
	if err := os.WriteFile(localPath, []byte(content), 0o644); err != nil {
		return err
	}
	hash := buildDiagnosticContentHash(content)
	artifact := &diagnosticBundleArtifact{
		StepCode:   step.Code,
		Category:   "config_snapshot",
		Format:     detectDiagnosticConfigFormat(fileName),
		Status:     "created",
		Path:       localPath,
		RemotePath: normalizedRemotePath,
		NodeID:     target.NodeID,
		HostID:     target.HostID,
		HostName:   target.HostName,
		SizeBytes:  int64(len(content)),
		Message:    configType,
	}
	state.Artifacts = append(state.Artifacts, artifact)
	summary.Files = append(summary.Files, diagnosticConfigSnapshotFile{
		HostID:      target.HostID,
		HostName:    target.HostName,
		HostIP:      target.HostIP,
		NodeID:      target.NodeID,
		Role:        target.Role,
		InstallDir:  target.InstallDir,
		ConfigType:  configType,
		RemotePath:  normalizedRemotePath,
		LocalPath:   localPath,
		SizeBytes:   int64(len(content)),
		ContentHash: hash,
	})
	if preview := buildDiagnosticConfigPreview(content); strings.TrimSpace(preview) != "" {
		summary.FilePreviews = append(summary.FilePreviews, diagnosticConfigFilePreview{
			HostID:     target.HostID,
			HostName:   target.HostName,
			HostIP:     target.HostIP,
			NodeID:     target.NodeID,
			Role:       target.Role,
			ConfigType: configType,
			RemotePath: normalizedRemotePath,
			Preview:    preview,
		})
	}
	if items := extractDiagnosticConfigHighlights(configType, normalizedRemotePath, content); len(items) > 0 {
		summary.KeyHighlights = append(summary.KeyHighlights, diagnosticConfigKeyHighlight{
			HostID:     target.HostID,
			HostName:   target.HostName,
			HostIP:     target.HostIP,
			NodeID:     target.NodeID,
			Role:       target.Role,
			ConfigType: configType,
			RemotePath: normalizedRemotePath,
			Items:      items,
		})
	}
	return s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
		TaskID:          task.ID,
		TaskStepID:      uintPtr(step.ID),
		NodeExecutionID: uintPtr(node.ID),
		StepCode:        step.Code,
		Level:           DiagnosticLogLevelInfo,
		EventType:       DiagnosticLogEventTypeSuccess,
		Message:         bilingualText(fmt.Sprintf("已采集 %s 到 %s", configType, localPath), fmt.Sprintf("Collected %s to %s", configType, localPath)),
		CommandSummary:  configType,
		CreatedAt:       time.Now().UTC(),
		Metadata: DiagnosticLogMetadata{
			"remote_path": normalizedRemotePath,
			"local_path":  localPath,
			"hash":        hash,
		},
	})
}

func (s *Service) collectDiagnosticDirectoryManifestArtifact(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, summary *diagnosticConfigSnapshotSummary, configDir string, target DiagnosticTaskNodeTarget, category string, manifest *agentDirectoryListingResult, node *DiagnosticNodeExecution) error {
	if manifest == nil {
		return nil
	}
	hostDir := filepath.Join(configDir, fmt.Sprintf("host-%d-%s", target.HostID, normalizeDiagnosticFileRole(target.Role)))
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return err
	}
	localPath := filepath.Join(hostDir, fmt.Sprintf("%s-manifest.json", strings.TrimSpace(category)))
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(localPath, payload, 0o644); err != nil {
		return err
	}
	state.Artifacts = append(state.Artifacts, &diagnosticBundleArtifact{
		StepCode:   step.Code,
		Category:   "directory_inventory",
		Format:     "json",
		Status:     "created",
		Path:       localPath,
		RemotePath: manifest.Path,
		NodeID:     target.NodeID,
		HostID:     target.HostID,
		HostName:   target.HostName,
		SizeBytes:  int64(len(payload)),
		Message:    category,
	})
	summary.DirectoryManifests = append(summary.DirectoryManifests, diagnosticDirectoryManifest{
		HostID:     target.HostID,
		HostName:   target.HostName,
		HostIP:     target.HostIP,
		NodeID:     target.NodeID,
		Role:       target.Role,
		Directory:  manifest.Path,
		LocalPath:  localPath,
		EntryCount: len(manifest.Entries),
		Entries:    manifest.Entries,
	})
	return s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
		TaskID:          task.ID,
		TaskStepID:      uintPtr(step.ID),
		NodeExecutionID: uintPtr(node.ID),
		StepCode:        step.Code,
		Level:           DiagnosticLogLevelInfo,
		EventType:       DiagnosticLogEventTypeSuccess,
		Message:         bilingualText(fmt.Sprintf("已采集 %s 目录清单到 %s", category, localPath), fmt.Sprintf("Collected %s directory manifest to %s", category, localPath)),
		CommandSummary:  category,
		CreatedAt:       time.Now().UTC(),
	})
}

type diagnosticConfigHighlightSpec struct {
	Label string
	Paths []string
}

func extractDiagnosticConfigHighlights(configType, remotePath, content string) []diagnosticConfigKeyValue {
	switch strings.TrimSpace(strings.ToLower(configType)) {
	case strings.ToLower(string(appconfig.ConfigTypeSeatunnel)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcast)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcastMaster)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcastWorker)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcastClient)):
		return extractDiagnosticYAMLHighlights(configType, content)
	case "jvm_options", "jvm_master_options", "jvm_worker_options", "jvm_client_options":
		return extractDiagnosticJVMOptionHighlights(content)
	case "log4j2.properties", "log4j2_client.properties":
		return extractDiagnosticLog4jHighlights(content)
	case "plugin_config":
		return extractDiagnosticPluginConfigHighlights(remotePath, content)
	default:
		return nil
	}
}

func extractDiagnosticYAMLHighlights(configType, content string) []diagnosticConfigKeyValue {
	var payload interface{}
	if err := yaml.Unmarshal([]byte(content), &payload); err != nil {
		return nil
	}
	flattened := make(map[string]string)
	flattenDiagnosticConfigValue("", payload, flattened)
	specs := buildDiagnosticYAMLHighlightSpecs(configType)
	result := make([]diagnosticConfigKeyValue, 0, len(specs))
	for _, spec := range specs {
		value := lookupDiagnosticConfigValue(flattened, spec.Paths...)
		if value == "" {
			continue
		}
		result = append(result, diagnosticConfigKeyValue{
			Label: spec.Label,
			Value: truncateString(normalizeDiagnosticDisplayText(value), 120),
		})
	}
	return result
}

func flattenDiagnosticConfigValue(prefix string, value interface{}, output map[string]string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			flattenDiagnosticConfigValue(joinDiagnosticConfigPath(prefix, key), item, output)
		}
	case map[interface{}]interface{}:
		for key, item := range typed {
			flattenDiagnosticConfigValue(joinDiagnosticConfigPath(prefix, fmt.Sprint(key)), item, output)
		}
	case []interface{}:
		if prefix == "" {
			return
		}
		scalars := make([]string, 0, len(typed))
		allScalars := true
		for _, item := range typed {
			switch item.(type) {
			case map[string]interface{}, map[interface{}]interface{}, []interface{}:
				allScalars = false
			default:
				scalars = append(scalars, strings.TrimSpace(fmt.Sprint(item)))
			}
		}
		if allScalars {
			output[strings.ToLower(prefix)] = summarizeDiagnosticScalarList(scalars)
			return
		}
		for index, item := range typed {
			flattenDiagnosticConfigValue(fmt.Sprintf("%s[%d]", prefix, index), item, output)
		}
	default:
		if prefix == "" {
			return
		}
		output[strings.ToLower(prefix)] = strings.TrimSpace(fmt.Sprint(value))
	}
}

func joinDiagnosticConfigPath(prefix, segment string) string {
	segment = strings.TrimSpace(strings.ToLower(segment))
	if prefix == "" {
		return segment
	}
	return prefix + "." + segment
}

func summarizeDiagnosticScalarList(items []string) string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		filtered = append(filtered, strings.TrimSpace(item))
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) <= 4 {
		return strings.Join(filtered, ", ")
	}
	return fmt.Sprintf("%s, ... (%d items)", strings.Join(filtered[:4], ", "), len(filtered))
}

func lookupDiagnosticConfigValue(flattened map[string]string, paths ...string) string {
	if len(flattened) == 0 {
		return ""
	}
	for _, path := range paths {
		normalizedPath := strings.TrimSpace(strings.ToLower(path))
		if normalizedPath == "" {
			continue
		}
		if value := strings.TrimSpace(flattened[normalizedPath]); value != "" {
			return value
		}
	}
	for _, path := range paths {
		normalizedPath := strings.TrimSpace(strings.ToLower(path))
		if normalizedPath == "" {
			continue
		}
		for key, value := range flattened {
			if strings.HasSuffix(key, normalizedPath) && strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func buildDiagnosticYAMLHighlightSpecs(configType string) []diagnosticConfigHighlightSpec {
	switch strings.TrimSpace(strings.ToLower(configType)) {
	case strings.ToLower(string(appconfig.ConfigTypeSeatunnel)):
		return []diagnosticConfigHighlightSpec{
			{Label: bilingualText("Metrics", "Metrics"), Paths: []string{"metrics.enabled"}},
			{Label: bilingualText("Prometheus", "Prometheus"), Paths: []string{"metrics.prometheus.enabled"}},
			{Label: bilingualText("JMX", "JMX"), Paths: []string{"metrics.jmx.enabled"}},
			{Label: bilingualText("备份副本", "Backup Count"), Paths: []string{"seatunnel.engine.backup-count"}},
			{Label: bilingualText("Checkpoint 间隔", "Checkpoint Interval"), Paths: []string{"seatunnel.engine.checkpoint.interval"}},
			{Label: bilingualText("HTTP 端口", "HTTP Port"), Paths: []string{"seatunnel.engine.http.port", "seatunnel.engine.rest.port"}},
		}
	case strings.ToLower(string(appconfig.ConfigTypeHazelcast)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcastMaster)),
		strings.ToLower(string(appconfig.ConfigTypeHazelcastWorker)):
		return []diagnosticConfigHighlightSpec{
			{Label: bilingualText("集群名", "Cluster Name"), Paths: []string{"hazelcast.cluster-name"}},
			{Label: bilingualText("监听端口", "Listen Port"), Paths: []string{"hazelcast.network.port.port"}},
			{Label: bilingualText("TCP/IP Join", "TCP/IP Join"), Paths: []string{"hazelcast.network.join.tcp-ip.enabled"}},
			{Label: bilingualText("Multicast", "Multicast"), Paths: []string{"hazelcast.network.join.multicast.enabled"}},
			{Label: bilingualText("Partition Group", "Partition Group"), Paths: []string{"hazelcast.partition-group.enabled"}},
			{Label: bilingualText("CP Member", "CP Member"), Paths: []string{"hazelcast.cp-subsystem.cp-member-count"}},
		}
	case strings.ToLower(string(appconfig.ConfigTypeHazelcastClient)):
		return []diagnosticConfigHighlightSpec{
			{Label: bilingualText("集群名", "Cluster Name"), Paths: []string{"hazelcast-client.cluster-name"}},
			{Label: bilingualText("客户端地址", "Client Addresses"), Paths: []string{"hazelcast-client.network.cluster-members", "hazelcast-client.network.addresses"}},
			{Label: bilingualText("重连模式", "Reconnect Mode"), Paths: []string{"hazelcast-client.connection-strategy.reconnect-mode"}},
			{Label: bilingualText("异步启动", "Async Start"), Paths: []string{"hazelcast-client.connection-strategy.async-start"}},
		}
	default:
		return nil
	}
}

func extractDiagnosticJVMOptionHighlights(content string) []diagnosticConfigKeyValue {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	lineMap := make(map[string]string)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "-Xms"):
			lineMap["xms"] = strings.TrimPrefix(line, "-Xms")
		case strings.HasPrefix(line, "-Xmx"):
			lineMap["xmx"] = strings.TrimPrefix(line, "-Xmx")
		case strings.HasPrefix(line, "-XX:MaxDirectMemorySize="):
			lineMap["direct_memory"] = strings.TrimPrefix(line, "-XX:MaxDirectMemorySize=")
		case strings.HasPrefix(line, "-XX:MaxMetaspaceSize="):
			lineMap["metaspace"] = strings.TrimPrefix(line, "-XX:MaxMetaspaceSize=")
		case strings.HasPrefix(line, "-XX:HeapDumpPath="):
			lineMap["heap_dump_path"] = strings.TrimPrefix(line, "-XX:HeapDumpPath=")
		case line == "-XX:+HeapDumpOnOutOfMemoryError":
			lineMap["heap_dump_on_oom"] = "true"
		case line == "-XX:+ExitOnOutOfMemoryError":
			lineMap["exit_on_oom"] = "true"
		case strings.HasPrefix(line, "-XX:OnOutOfMemoryError="):
			lineMap["on_oom"] = strings.TrimPrefix(line, "-XX:OnOutOfMemoryError=")
		}
	}
	specs := []struct {
		key   string
		label string
	}{
		{key: "xms", label: "Xms"},
		{key: "xmx", label: "Xmx"},
		{key: "direct_memory", label: bilingualText("直接内存", "Direct Memory")},
		{key: "metaspace", label: bilingualText("Metaspace", "Metaspace")},
		{key: "heap_dump_on_oom", label: bilingualText("OOM HeapDump", "OOM HeapDump")},
		{key: "heap_dump_path", label: bilingualText("HeapDump 路径", "HeapDump Path")},
		{key: "exit_on_oom", label: bilingualText("OOM 退出", "Exit On OOM")},
		{key: "on_oom", label: bilingualText("OOM Hook", "OOM Hook")},
	}
	result := make([]diagnosticConfigKeyValue, 0, len(specs))
	for _, spec := range specs {
		if value := strings.TrimSpace(lineMap[spec.key]); value != "" {
			result = append(result, diagnosticConfigKeyValue{
				Label: spec.label,
				Value: truncateString(value, 120),
			})
		}
	}
	return result
}

func extractDiagnosticLog4jHighlights(content string) []diagnosticConfigKeyValue {
	properties := parseDiagnosticProperties(content)
	if len(properties) == 0 {
		return nil
	}
	result := make([]diagnosticConfigKeyValue, 0, 4)
	if level := strings.TrimSpace(properties["rootlogger.level"]); level != "" {
		result = append(result, diagnosticConfigKeyValue{
			Label: bilingualText("Root 日志级别", "Root Log Level"),
			Value: truncateString(level, 120),
		})
	}
	if fileName := firstNonEmptyString(properties["appender.rolling.filename"], properties["appender.file.filename"]); strings.TrimSpace(fileName) != "" {
		result = append(result, diagnosticConfigKeyValue{
			Label: bilingualText("主日志文件", "Primary Log File"),
			Value: truncateString(fileName, 120),
		})
	}
	routingEnabled := false
	routingPattern := ""
	for key, value := range properties {
		if strings.Contains(key, "routing") {
			routingEnabled = true
		}
		if key == "appender.routing.routes.pattern" {
			routingPattern = value
		}
	}
	if routingEnabled {
		value := bilingualText("已启用", "enabled")
		if strings.TrimSpace(routingPattern) != "" {
			value = truncateString(fmt.Sprintf("%s (%s)", value, routingPattern), 120)
		}
		result = append(result, diagnosticConfigKeyValue{
			Label: bilingualText("RoutingAppender", "RoutingAppender"),
			Value: value,
		})
	}
	return result
}

func parseDiagnosticProperties(content string) map[string]string {
	properties := make(map[string]string)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		index := strings.IndexAny(line, "=:")
		if index <= 0 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(line[:index]))
		value := strings.TrimSpace(line[index+1:])
		if key == "" || value == "" {
			continue
		}
		properties[key] = value
	}
	return properties
}

func extractDiagnosticPluginConfigHighlights(remotePath, content string) []diagnosticConfigKeyValue {
	count := 0
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	if count == 0 {
		return nil
	}
	return []diagnosticConfigKeyValue{
		{
			Label: bilingualText("插件规则数", "Plugin Rules"),
			Value: fmt.Sprintf("%d", count),
		},
		{
			Label: bilingualText("来源文件", "Source File"),
			Value: truncateString(filepath.Base(strings.TrimSpace(remotePath)), 120),
		},
	}
}

func buildDiagnosticConfigPreview(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return strings.TrimSpace(content)
}

func buildDiagnosticExtraConfigFilesForTarget(mode cluster.DeploymentMode, role string) []string {
	items := []string{
		"log4j2.properties",
		"log4j2_client.properties",
		"plugin_config",
		"jvm_client_options",
	}
	switch mode {
	case cluster.DeploymentModeSeparated:
		switch strings.TrimSpace(role) {
		case string(cluster.NodeRoleMaster):
			items = append(items, "jvm_master_options")
		case string(cluster.NodeRoleWorker):
			items = append(items, "jvm_worker_options")
		case string(cluster.NodeRoleMasterWorker):
			items = append(items, "jvm_master_options", "jvm_worker_options")
		default:
			items = append(items, "jvm_master_options", "jvm_worker_options")
		}
	default:
		items = append(items, "jvm_options")
	}
	return deduplicateDiagnosticStrings(items)
}

func buildDiagnosticConfigChangeRecords(configs []*appconfig.Config) []diagnosticConfigChangeRecord {
	if len(configs) == 0 {
		return []diagnosticConfigChangeRecord{}
	}
	result := make([]diagnosticConfigChangeRecord, 0, len(configs))
	for _, item := range configs {
		if item == nil {
			continue
		}
		hostID := item.HostID
		result = append(result, diagnosticConfigChangeRecord{
			ConfigID:   item.ID,
			ClusterID:  item.ClusterID,
			HostID:     hostID,
			HostScope:  buildDiagnosticConfigChangeScope(item),
			ConfigType: string(item.ConfigType),
			FilePath:   item.FilePath,
			Version:    item.Version,
			UpdatedAt:  item.UpdatedAt,
			UpdatedBy:  item.UpdatedBy,
			IsTemplate: item.IsTemplate(),
		})
	}
	return result
}

func buildDiagnosticConfigChangeScope(item *appconfig.Config) string {
	if item == nil {
		return "-"
	}
	if item.IsTemplate() {
		return "template"
	}
	if item.HostID != nil {
		return fmt.Sprintf("host #%d", *item.HostID)
	}
	return "node"
}

func buildDiagnosticConfigTypesForTarget(mode cluster.DeploymentMode, role string) []appconfig.ConfigType {
	result := []appconfig.ConfigType{appconfig.ConfigTypeSeatunnel}
	switch mode {
	case cluster.DeploymentModeSeparated:
		switch strings.TrimSpace(role) {
		case string(cluster.NodeRoleMaster):
			result = append(result, appconfig.ConfigTypeHazelcastMaster)
		case string(cluster.NodeRoleWorker):
			result = append(result, appconfig.ConfigTypeHazelcastWorker)
		case string(cluster.NodeRoleMasterWorker):
			result = append(result, appconfig.ConfigTypeHazelcastMaster, appconfig.ConfigTypeHazelcastWorker)
		default:
			result = append(result, appconfig.ConfigTypeHazelcastMaster, appconfig.ConfigTypeHazelcastWorker)
		}
	default:
		result = append(result, appconfig.ConfigTypeHazelcast)
	}
	result = append(result, appconfig.ConfigTypeHazelcastClient)
	return deduplicateDiagnosticConfigTypes(result)
}

func deduplicateDiagnosticConfigTypes(items []appconfig.ConfigType) []appconfig.ConfigType {
	if len(items) == 0 {
		return []appconfig.ConfigType{}
	}
	result := make([]appconfig.ConfigType, 0, len(items))
	seen := make(map[appconfig.ConfigType]struct{}, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func deduplicateDiagnosticStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func detectDiagnosticConfigFormat(name string) string {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	switch {
	case strings.HasSuffix(trimmed, ".yaml"), strings.HasSuffix(trimmed, ".yml"):
		return "yaml"
	case strings.HasSuffix(trimmed, ".properties"):
		return "properties"
	default:
		return "text"
	}
}

func buildDiagnosticContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:8])
}

func (s *Service) executeCollectLogSampleStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, nodesByClusterNodeID map[uint]*DiagnosticNodeExecution, state *diagnosticBundleExecutionState, bundleDir string) error {
	if s.agentSender == nil {
		return fmt.Errorf("agent sender is unavailable")
	}
	window := resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
	state.WindowStart = timePtr(window.Start)
	state.WindowEnd = timePtr(window.End)
	state.LookbackMinutes = window.LookbackMinutes
	logDir := filepath.Join(bundleDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	var successCount int
	var errs []string
	for _, selected := range sortedDiagnosticTaskTargets(task.SelectedNodes) {
		node := nodesByClusterNodeID[selected.ClusterNodeID]
		if node == nil {
			continue
		}
		if err := s.beginDiagnosticNodeStep(ctx, step, node, bilingualText("正在采集日志样本。", "Collecting log samples.")); err != nil {
			return err
		}
		candidates := buildDiagnosticLogCandidates(selected, state.ErrorEvents)
		var nodeSuccess bool
		for _, candidate := range candidates {
			snippet, detail, err := s.collectDiagnosticWindowedLogSnippet(ctx, selected, candidate, window, task.Options.LogSampleLines)
			if err != nil || strings.TrimSpace(snippet) == "" {
				detail = firstNonEmptyString(detail, bilingualText("日志样本采集失败。", "Failed to collect log sample."))
				_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
					TaskID:          task.ID,
					TaskStepID:      uintPtr(step.ID),
					NodeExecutionID: uintPtr(node.ID),
					StepCode:        step.Code,
					Level:           DiagnosticLogLevelWarn,
					EventType:       DiagnosticLogEventTypeNote,
					Message:         bilingualText(fmt.Sprintf("从 %s 采集日志样本失败：%s", candidate, detail), fmt.Sprintf("Failed to collect log sample from %s: %s", candidate, detail)),
					CommandSummary:  candidate,
					CreatedAt:       time.Now().UTC(),
				})
				continue
			}
			fileName := fmt.Sprintf("host-%d-%s.log", selected.HostID, filepath.Base(candidate))
			localPath := filepath.Join(logDir, fileName)
			if err := os.WriteFile(localPath, []byte(snippet), 0o644); err != nil {
				return err
			}
			state.Artifacts = append(state.Artifacts, &diagnosticBundleArtifact{
				StepCode:  step.Code,
				Category:  "log_sample",
				Format:    "log",
				Status:    "created",
				Path:      localPath,
				NodeID:    selected.NodeID,
				HostID:    selected.HostID,
				HostName:  selected.HostName,
				SizeBytes: int64(len(snippet)),
				Message:   candidate,
			})
			state.LogSamples = append(state.LogSamples, diagnosticCollectedLogSample{
				HostID:      selected.HostID,
				HostName:    selected.HostName,
				HostIP:      selected.HostIP,
				NodeID:      selected.NodeID,
				Role:        selected.Role,
				SourceFile:  candidate,
				LocalPath:   localPath,
				WindowStart: window.Start,
				WindowEnd:   window.End,
				Content:     snippet,
			})
			_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
				TaskID:          task.ID,
				TaskStepID:      uintPtr(step.ID),
				NodeExecutionID: uintPtr(node.ID),
				StepCode:        step.Code,
				Level:           DiagnosticLogLevelInfo,
				EventType:       DiagnosticLogEventTypeSuccess,
				Message:         bilingualText(fmt.Sprintf("已从 %s 采集日志样本", candidate), fmt.Sprintf("Collected log sample from %s", candidate)),
				CommandSummary:  candidate,
				CreatedAt:       time.Now().UTC(),
			})
			nodeSuccess = true
			successCount++
			break
		}
		if nodeSuccess {
			if err := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusSucceeded, bilingualText("日志样本采集完成。", "Log sample collected.")); err != nil {
				return err
			}
			continue
		}
		errs = append(errs, fmt.Sprintf("host=%d", selected.HostID))
		if err := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusFailed, bilingualText("未采集到日志样本。", "No log sample collected.")); err != nil {
			return err
		}
	}
	if successCount == 0 {
		return formatDiagnosticAllNodesFailed("全部节点都未采集到日志样本", "No log samples collected on any node", errs)
	}
	sort.SliceStable(state.LogSamples, func(i, j int) bool {
		if state.LogSamples[i].HostID != state.LogSamples[j].HostID {
			return state.LogSamples[i].HostID < state.LogSamples[j].HostID
		}
		return state.LogSamples[i].SourceFile < state.LogSamples[j].SourceFile
	})
	return nil
}

func (s *Service) executeCollectThreadDumpStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, nodesByClusterNodeID map[uint]*DiagnosticNodeExecution, state *diagnosticBundleExecutionState, bundleDir string) error {
	if s.agentSender == nil {
		return fmt.Errorf("agent sender is unavailable")
	}
	outputDir := filepath.Join(bundleDir, "thread-dumps")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	var successCount int
	var errs []string
	for _, selected := range sortedDiagnosticTaskTargets(task.SelectedNodes) {
		node := nodesByClusterNodeID[selected.ClusterNodeID]
		if node == nil {
			continue
		}
		if err := s.beginDiagnosticNodeStep(ctx, step, node, bilingualText("正在采集线程栈。", "Collecting thread dump.")); err != nil {
			return err
		}
		success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, selected.AgentID, "thread_dump", map[string]string{
			"install_dir": selected.InstallDir,
			"role":        selected.Role,
		})
		if err != nil || !success {
			detail := resolveDiagnosticCommandFailure(output, err, "线程栈采集失败。", "Thread dump failed.")
			errs = append(errs, fmt.Sprintf("host=%d: %s", selected.HostID, detail))
			_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
				TaskID:          task.ID,
				TaskStepID:      uintPtr(step.ID),
				NodeExecutionID: uintPtr(node.ID),
				StepCode:        step.Code,
				Level:           DiagnosticLogLevelError,
				EventType:       DiagnosticLogEventTypeFailed,
				Message:         detail,
				CommandSummary:  selected.Role,
				CreatedAt:       time.Now().UTC(),
			})
			if finishErr := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusFailed, detail); finishErr != nil {
				return finishErr
			}
			continue
		}
		var result agentThreadDumpResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return err
		}
		localPath := filepath.Join(outputDir, fmt.Sprintf("thread-dump-host-%d-%s.txt", selected.HostID, normalizeDiagnosticFileRole(selected.Role)))
		if err := os.WriteFile(localPath, []byte(result.Content), 0o644); err != nil {
			return err
		}
		state.Artifacts = append(state.Artifacts, &diagnosticBundleArtifact{
			StepCode:   step.Code,
			Category:   "thread_dump",
			Format:     "txt",
			Status:     result.Status,
			Path:       localPath,
			RemotePath: result.OutputPath,
			NodeID:     selected.NodeID,
			HostID:     selected.HostID,
			HostName:   selected.HostName,
			SizeBytes:  result.SizeBytes,
			Message:    result.Tool,
		})
		if err := s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
			TaskID:          task.ID,
			TaskStepID:      uintPtr(step.ID),
			NodeExecutionID: uintPtr(node.ID),
			StepCode:        step.Code,
			Level:           DiagnosticLogLevelInfo,
			EventType:       DiagnosticLogEventTypeSuccess,
			Message:         bilingualText(fmt.Sprintf("线程栈已保存到 %s", localPath), fmt.Sprintf("Thread dump collected to %s", localPath)),
			CommandSummary:  result.Tool,
			CreatedAt:       time.Now().UTC(),
			Metadata: DiagnosticLogMetadata{
				"remote_path": result.OutputPath,
			},
		}); err != nil {
			return err
		}
		if err := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusSucceeded, bilingualText("线程栈采集完成。", "Thread dump collected.")); err != nil {
			return err
		}
		successCount++
	}
	if successCount == 0 {
		return formatDiagnosticAllNodesFailed("全部节点线程栈采集失败", "Thread dump failed on all nodes", errs)
	}
	return nil
}

func (s *Service) executeCollectJVMDumpStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, nodesByClusterNodeID map[uint]*DiagnosticNodeExecution, state *diagnosticBundleExecutionState, bundleDir string) error {
	if s.agentSender == nil {
		return fmt.Errorf("agent sender is unavailable")
	}
	outputDir := filepath.Join(bundleDir, "jvm-dumps")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	var successCount int
	var skippedCount int
	var errs []string
	for _, selected := range sortedDiagnosticTaskTargets(task.SelectedNodes) {
		node := nodesByClusterNodeID[selected.ClusterNodeID]
		if node == nil {
			continue
		}
		if err := s.beginDiagnosticNodeStep(ctx, step, node, bilingualText("正在采集 JVM Dump。", "Collecting JVM dump.")); err != nil {
			return err
		}
		success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, selected.AgentID, "jvm_dump", map[string]string{
			"install_dir": selected.InstallDir,
			"role":        selected.Role,
			"min_free_mb": fmt.Sprintf("%d", task.Options.JVMDumpMinFreeMB),
		})
		if err != nil || !success {
			detail := resolveDiagnosticCommandFailure(output, err, "JVM Dump 采集失败。", "JVM dump failed.")
			errs = append(errs, fmt.Sprintf("host=%d: %s", selected.HostID, detail))
			_ = s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
				TaskID:          task.ID,
				TaskStepID:      uintPtr(step.ID),
				NodeExecutionID: uintPtr(node.ID),
				StepCode:        step.Code,
				Level:           DiagnosticLogLevelError,
				EventType:       DiagnosticLogEventTypeFailed,
				Message:         detail,
				CommandSummary:  selected.Role,
				CreatedAt:       time.Now().UTC(),
			})
			if finishErr := s.finishDiagnosticNodeStep(ctx, step, node, DiagnosticTaskStatusFailed, detail); finishErr != nil {
				return finishErr
			}
			continue
		}
		var result agentJVMDumpResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			return err
		}
		localPath := filepath.Join(outputDir, fmt.Sprintf("jvm-dump-host-%d-%s.json", selected.HostID, normalizeDiagnosticFileRole(selected.Role)))
		if err := os.WriteFile(localPath, []byte(output), 0o644); err != nil {
			return err
		}
		status := DiagnosticTaskStatusSucceeded
		if result.Status == "skipped" {
			status = DiagnosticTaskStatusSkipped
			skippedCount++
		} else {
			successCount++
		}
		state.Artifacts = append(state.Artifacts, &diagnosticBundleArtifact{
			StepCode:   step.Code,
			Category:   "jvm_dump",
			Format:     "json",
			Status:     result.Status,
			Path:       localPath,
			RemotePath: result.OutputPath,
			NodeID:     selected.NodeID,
			HostID:     selected.HostID,
			HostName:   selected.HostName,
			SizeBytes:  result.SizeBytes,
			Message:    result.Message,
		})
		if err := s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
			TaskID:          task.ID,
			TaskStepID:      uintPtr(step.ID),
			NodeExecutionID: uintPtr(node.ID),
			StepCode:        step.Code,
			Level:           DiagnosticLogLevelInfo,
			EventType:       DiagnosticLogEventTypeNote,
			Message:         result.Message,
			CommandSummary:  result.Tool,
			CreatedAt:       time.Now().UTC(),
			Metadata: DiagnosticLogMetadata{
				"remote_path":    result.OutputPath,
				"free_bytes":     result.FreeBytes,
				"required_bytes": result.RequiredBytes,
			},
		}); err != nil {
			return err
		}
		if err := s.finishDiagnosticNodeStep(ctx, step, node, status, result.Message); err != nil {
			return err
		}
	}
	if successCount == 0 && skippedCount == 0 {
		return formatDiagnosticAllNodesFailed("全部节点 JVM Dump 采集失败", "JVM dump failed on all nodes", errs)
	}
	return nil
}

func (s *Service) executeAssembleManifestStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir string) error {
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestArtifact := &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "manifest",
		Format:   "json",
		Status:   "created",
		Path:     manifestPath,
		Message:  bilingualText("诊断包 Manifest", "Diagnostic bundle manifest"),
	}
	if err := writeDiagnosticBundleManifestFile(manifestPath, task, append(cloneDiagnosticArtifacts(state.Artifacts), manifestArtifact), state); err != nil {
		return err
	}
	fileInfo, err := os.Stat(manifestPath)
	if err != nil {
		return err
	}
	manifestArtifact.SizeBytes = fileInfo.Size()
	task.ManifestPath = manifestPath
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}
	state.Artifacts = append(state.Artifacts, manifestArtifact)
	return nil
}

func (s *Service) executeRenderHTMLSummaryStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir string) error {
	indexPath := filepath.Join(bundleDir, "index.html")
	htmlArtifact := &diagnosticBundleArtifact{
		StepCode: step.Code,
		Category: "diagnostic_report",
		Format:   "html",
		Status:   "created",
		Path:     indexPath,
		Message:  bilingualText("离线诊断报告", "Offline diagnostic report"),
	}
	payload := buildDiagnosticBundleHTMLPayload(task, state, bundleDir, append(cloneDiagnosticArtifacts(state.Artifacts), htmlArtifact))
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
		"metricChartSVG": func(points []diagnosticPrometheusPoint, threshold float64, comparator string, unit string) template.HTML {
			return renderDiagnosticMetricChart(points, threshold, comparator, unit)
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
		return err
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, payload); err != nil {
		return err
	}
	if err := os.WriteFile(indexPath, buffer.Bytes(), 0o644); err != nil {
		return err
	}
	task.IndexPath = indexPath
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}
	htmlArtifact.SizeBytes = int64(buffer.Len())
	state.Artifacts = append(state.Artifacts, htmlArtifact)
	if task.ManifestPath != "" {
		if err := writeDiagnosticBundleManifestFile(task.ManifestPath, task, state.Artifacts, state); err != nil {
			return err
		}
	}
	return nil
}

func resolveDiagnosticCollectionWindow(task *DiagnosticTask, inspectionDetail *ClusterInspectionReportDetailData) diagnosticCollectionWindow {
	end := time.Now().UTC()
	lookbackMinutes := 0
	if inspectionDetail != nil && inspectionDetail.Report != nil {
		report := inspectionDetail.Report
		if report.FinishedAt != nil && !report.FinishedAt.IsZero() {
			end = report.FinishedAt.UTC()
		} else if report.StartedAt != nil && !report.StartedAt.IsZero() {
			end = report.StartedAt.UTC()
		}
		if report.LookbackMinutes >= minInspectionLookbackMinutes && report.LookbackMinutes <= maxInspectionLookbackMinutes {
			lookbackMinutes = report.LookbackMinutes
		}
	}
	if task != nil && task.LookbackMinutes >= minInspectionLookbackMinutes && task.LookbackMinutes <= maxInspectionLookbackMinutes {
		lookbackMinutes = task.LookbackMinutes
	}
	if lookbackMinutes == 0 {
		lookbackMinutes = defaultInspectionLookbackMinutes
	}
	start := end.Add(-time.Duration(lookbackMinutes) * time.Minute)
	return diagnosticCollectionWindow{
		Start:           start,
		End:             end,
		LookbackMinutes: lookbackMinutes,
	}
}

func filterDiagnosticProcessEventsByWindow(events []*monitor.ProcessEvent, start, end time.Time) []*monitor.ProcessEvent {
	if len(events) == 0 {
		return []*monitor.ProcessEvent{}
	}
	filtered := make([]*monitor.ProcessEvent, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		createdAt := event.CreatedAt.UTC()
		if createdAt.Before(start) || createdAt.After(end) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func filterDiagnosticAlertsByWindow(alerts []*monitoringapp.AlertInstance, start, end time.Time, sourceAlertID string) []*monitoringapp.AlertInstance {
	if len(alerts) == 0 {
		return []*monitoringapp.AlertInstance{}
	}
	sourceAlertID = strings.TrimSpace(sourceAlertID)
	filtered := make([]*monitoringapp.AlertInstance, 0, len(alerts))
	for _, alert := range alerts {
		if alert == nil {
			continue
		}
		if sourceAlertID != "" && strings.TrimSpace(alert.AlertID) == sourceAlertID {
			filtered = append(filtered, alert)
			continue
		}
		if shouldIncludeDiagnosticAlert(alert, start, end) {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

func shouldIncludeDiagnosticAlert(alert *monitoringapp.AlertInstance, start, end time.Time) bool {
	if alert == nil {
		return false
	}
	switch alert.Status {
	case monitoringapp.AlertDisplayStatusFiring:
		firingAt := alert.FiringAt.UTC()
		lastSeenAt := alert.LastSeenAt.UTC()
		return !firingAt.After(end) && !lastSeenAt.Before(start)
	case monitoringapp.AlertDisplayStatusResolved:
		if alert.ResolvedAt != nil && !alert.ResolvedAt.IsZero() {
			resolvedAt := alert.ResolvedAt.UTC()
			return !resolvedAt.Before(start) && !resolvedAt.After(end)
		}
		lastSeenAt := alert.LastSeenAt.UTC()
		return !lastSeenAt.Before(start) && !lastSeenAt.After(end)
	default:
		return false
	}
}

func timePtr(value time.Time) *time.Time {
	normalized := value.UTC()
	return &normalized
}

type diagnosticPrometheusSignalSpec struct {
	Key            string
	Title          string
	Unit           string
	Threshold      float64
	ThresholdText  string
	StatusOnBreach string
	Comparator     string
	PromQL         string
}

type diagnosticPrometheusQueryRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string                                 `json:"resultType"`
		Result     []diagnosticPrometheusQueryRangeVector `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

type diagnosticPrometheusQueryRangeVector struct {
	Metric map[string]string `json:"metric"`
	Values [][]interface{}   `json:"values"`
}

func (s *Service) collectDiagnosticPrometheusSnapshot(ctx context.Context, task *DiagnosticTask, window diagnosticCollectionWindow) (*diagnosticPrometheusSnapshot, error) {
	baseURL := strings.TrimSpace(config.Config.Observability.Prometheus.URL)
	if baseURL == "" || task == nil || task.ClusterID == 0 {
		return nil, nil
	}
	specs := buildDiagnosticPrometheusSignalSpecs(task.ClusterID)
	if len(specs) == 0 {
		return nil, nil
	}
	stepSeconds := computeDiagnosticPrometheusStepSeconds(window)
	snapshot := &diagnosticPrometheusSnapshot{
		ClusterID:   task.ClusterID,
		WindowStart: window.Start,
		WindowEnd:   window.End,
		StepSeconds: stepSeconds,
		Signals:     make([]diagnosticPrometheusSignal, 0, len(specs)),
	}
	for _, spec := range specs {
		signal, err := queryDiagnosticPrometheusSignal(ctx, baseURL, window, stepSeconds, spec)
		if err != nil {
			snapshot.CollectionNotes = append(snapshot.CollectionNotes, fmt.Sprintf("%s: %v", spec.Key, err))
			continue
		}
		snapshot.Signals = append(snapshot.Signals, *signal)
	}
	if len(snapshot.Signals) == 0 && len(snapshot.CollectionNotes) == 0 {
		return nil, nil
	}
	return snapshot, nil
}

func buildDiagnosticPrometheusSignalSpecs(clusterID uint) []diagnosticPrometheusSignalSpec {
	matchers := []string{
		`job="seatunnel_engine_http"`,
		fmt.Sprintf(`cluster_id="%d"`, clusterID),
	}
	oldGenPoolMatcher := `pool=~"G1 Old Gen|PS Old Gen|CMS Old Gen|Tenured Gen"`
	selector := func(metric string, extra ...string) string {
		items := make([]string, 0, len(matchers)+len(extra))
		items = append(items, matchers...)
		items = append(items, extra...)
		return buildDiagnosticPrometheusSelector(metric, items...)
	}
	return []diagnosticPrometheusSignalSpec{
		{
			Key:            "cpu_usage_high",
			Title:          bilingualText("CPU 负载", "CPU Load"),
			Unit:           "cores",
			Threshold:      0.8,
			ThresholdText:  "> 0.8 cores",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL:         fmt.Sprintf(`sum by (instance) (rate(%s[5m]))`, selector("process_cpu_seconds_total")),
		},
		{
			Key:            "memory_usage_high",
			Title:          bilingualText("JVM Heap 使用率", "JVM Heap Usage"),
			Unit:           "ratio",
			Threshold:      0.8,
			ThresholdText:  "> 80%",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL: fmt.Sprintf(`max by (instance) ((%s) / clamp_min((%s), 1))`,
				selector("jvm_memory_bytes_used", `area="heap"`),
				selector("jvm_memory_bytes_max", `area="heap"`),
			),
		},
		{
			Key:            "fd_usage_high",
			Title:          bilingualText("FD 使用率", "FD Usage"),
			Unit:           "ratio",
			Threshold:      0.8,
			ThresholdText:  "> 80%",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL: fmt.Sprintf(`max by (instance) ((%s) / clamp_min((%s), 1))`,
				selector("process_open_fds"),
				selector("process_max_fds"),
			),
		},
		{
			Key:            "old_gen_usage_high",
			Title:          bilingualText("Old Gen 使用率", "Old Gen Usage"),
			Unit:           "ratio",
			Threshold:      0.8,
			ThresholdText:  "> 80%",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL: fmt.Sprintf(`max by (instance) ((%s) / clamp_min((%s), 1))`,
				selector("jvm_memory_pool_bytes_used", oldGenPoolMatcher),
				selector("jvm_memory_pool_bytes_max", oldGenPoolMatcher),
			),
		},
		{
			Key:            "gc_time_ratio_high",
			Title:          bilingualText("GC 时间占比", "GC Time Ratio"),
			Unit:           "percent",
			Threshold:      10,
			ThresholdText:  "> 10% (5m)",
			StatusOnBreach: "warning",
			Comparator:     "gt",
			PromQL:         fmt.Sprintf(`100 * sum by (instance) (rate(%s[5m]))`, selector("jvm_gc_collection_seconds_sum")),
		},
		{
			Key:            "deadlocked_threads_detected",
			Title:          bilingualText("死锁线程", "Deadlocked Threads"),
			Unit:           "count",
			Threshold:      0,
			ThresholdText:  "> 0",
			StatusOnBreach: "critical",
			Comparator:     "gt",
			PromQL:         fmt.Sprintf(`max by (instance) (%s)`, selector("jvm_threads_deadlocked")),
		},
		{
			Key:            "job_thread_pool_rejection_high",
			Title:          bilingualText("作业线程池拒绝数", "Job Thread Pool Rejections"),
			Unit:           "count",
			Threshold:      0,
			ThresholdText:  "> 0",
			StatusOnBreach: "critical",
			Comparator:     "gt",
			PromQL:         fmt.Sprintf(`sum by (instance) (increase(%s[5m]))`, selector("job_thread_pool_rejection_total")),
		},
		{
			Key:            "split_brain_risk",
			Title:          bilingualText("Hazelcast 分区安全", "Hazelcast Partition Safety"),
			Unit:           "bool",
			Threshold:      1,
			ThresholdText:  "< 1",
			StatusOnBreach: "critical",
			Comparator:     "lt",
			PromQL:         fmt.Sprintf(`min by (instance) (%s)`, selector("hazelcast_partition_isClusterSafe")),
		},
	}
}

func queryDiagnosticPrometheusSignal(ctx context.Context, baseURL string, window diagnosticCollectionWindow, stepSeconds int, spec diagnosticPrometheusSignalSpec) (*diagnosticPrometheusSignal, error) {
	series, err := queryDiagnosticPrometheusRange(ctx, baseURL, spec.PromQL, window.Start, window.End, stepSeconds)
	if err != nil {
		return nil, err
	}
	summaries := make([]diagnosticPrometheusSeriesSummary, 0, len(series))
	breachCount := 0
	for _, item := range series {
		summary, breached := summarizeDiagnosticPrometheusSeries(item, spec.Comparator, spec.Threshold)
		if summary.Samples == 0 {
			continue
		}
		summaries = append(summaries, summary)
		if breached {
			breachCount++
		}
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		return compareDiagnosticPrometheusSeries(summaries[i], summaries[j], spec.Comparator)
	})
	if len(summaries) > 5 {
		summaries = summaries[:5]
	}
	status := "healthy"
	summaryText := bilingualText("诊断窗口内未发现明显异常。", "No significant anomaly was detected in the diagnostics window.")
	if breachCount > 0 {
		status = spec.StatusOnBreach
		top := "-"
		if len(summaries) > 0 {
			top = summaries[0].Instance
		}
		summaryText = bilingualText(
			fmt.Sprintf("诊断窗口内共有 %d 个实例触达阈值，最突出实例：%s。", breachCount, top),
			fmt.Sprintf("%d instance(s) breached the threshold in this window, top instance: %s.", breachCount, top),
		)
	}
	return &diagnosticPrometheusSignal{
		Key:           spec.Key,
		Title:         spec.Title,
		Summary:       summaryText,
		PromQL:        spec.PromQL,
		Unit:          spec.Unit,
		Threshold:     spec.Threshold,
		ThresholdText: spec.ThresholdText,
		Status:        status,
		Comparator:    spec.Comparator,
		Series:        summaries,
	}, nil
}

func queryDiagnosticPrometheusRange(ctx context.Context, baseURL, promQL string, start, end time.Time, stepSeconds int) ([]diagnosticPrometheusQueryRangeVector, error) {
	apiURL := joinDiagnosticPrometheusURL(baseURL, "/api/v1/query_range")
	if apiURL == "" {
		return nil, fmt.Errorf("prometheus url is empty")
	}
	params := url.Values{}
	params.Set("query", promQL)
	params.Set("start", strconv.FormatFloat(float64(start.UTC().Unix()), 'f', -1, 64))
	params.Set("end", strconv.FormatFloat(float64(end.UTC().Unix()), 'f', -1, 64))
	params.Set("step", strconv.Itoa(stepSeconds))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("prometheus query_range status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var payload diagnosticPrometheusQueryRangeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Status), "success") {
		return nil, fmt.Errorf("prometheus query_range returned status %q: %s", payload.Status, strings.TrimSpace(payload.Error))
	}
	return payload.Data.Result, nil
}

func summarizeDiagnosticPrometheusSeries(item diagnosticPrometheusQueryRangeVector, comparator string, threshold float64) (diagnosticPrometheusSeriesSummary, bool) {
	summary := diagnosticPrometheusSeriesSummary{
		Instance: firstNonEmptyString(strings.TrimSpace(item.Metric["instance"]), strings.TrimSpace(item.Metric["cluster"]), "-"),
		MinValue: 0,
		MaxValue: 0,
		Points:   make([]diagnosticPrometheusPoint, 0, len(item.Values)),
	}
	if len(item.Values) == 0 {
		return summary, false
	}
	first := true
	breached := false
	for _, sample := range item.Values {
		if len(sample) < 2 {
			continue
		}
		value, ok := parseDiagnosticPrometheusSampleValue(sample[1])
		if !ok {
			continue
		}
		ts, tsOK := parseDiagnosticPrometheusSampleValue(sample[0])
		if first {
			summary.MinValue = value
			summary.MaxValue = value
			if tsOK {
				sampledAt := time.Unix(int64(ts), 0).UTC()
				summary.MinAt = &sampledAt
				summary.MaxAt = &sampledAt
			}
			first = false
		}
		if value < summary.MinValue {
			summary.MinValue = value
			if tsOK {
				sampledAt := time.Unix(int64(ts), 0).UTC()
				summary.MinAt = &sampledAt
			}
		}
		if value > summary.MaxValue {
			summary.MaxValue = value
			if tsOK {
				sampledAt := time.Unix(int64(ts), 0).UTC()
				summary.MaxAt = &sampledAt
			}
		}
		summary.LastValue = value
		if tsOK {
			sampledAt := time.Unix(int64(ts), 0).UTC()
			summary.LastAt = &sampledAt
			summary.Points = append(summary.Points, diagnosticPrometheusPoint{
				Timestamp: sampledAt,
				Value:     value,
			})
		}
		summary.Samples++
		switch comparator {
		case "lt":
			if value < threshold {
				breached = true
			}
		default:
			if value > threshold {
				breached = true
			}
		}
	}
	return summary, breached
}

func compareDiagnosticPrometheusSeries(left, right diagnosticPrometheusSeriesSummary, comparator string) bool {
	switch comparator {
	case "lt":
		if left.MinValue != right.MinValue {
			return left.MinValue < right.MinValue
		}
	default:
		if left.MaxValue != right.MaxValue {
			return left.MaxValue > right.MaxValue
		}
	}
	return left.Instance < right.Instance
}

func parseDiagnosticPrometheusSampleValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func computeDiagnosticPrometheusStepSeconds(window diagnosticCollectionWindow) int {
	totalSeconds := int(window.End.Sub(window.Start).Seconds())
	if totalSeconds <= 0 {
		return 60
	}
	step := totalSeconds / 24
	if step < 60 {
		step = 60
	}
	if step > 300 {
		step = 300
	}
	return step
}

func buildDiagnosticPrometheusSelector(metric string, matchers ...string) string {
	items := make([]string, 0, len(matchers))
	for _, matcher := range matchers {
		matcher = strings.TrimSpace(matcher)
		if matcher == "" {
			continue
		}
		items = append(items, matcher)
	}
	if len(items) == 0 {
		return strings.TrimSpace(metric)
	}
	return fmt.Sprintf("%s{%s}", strings.TrimSpace(metric), strings.Join(items, ","))
}

func joinDiagnosticPrometheusURL(baseURL, suffix string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	if suffix == "" {
		return base
	}
	if !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}
	return base + suffix
}

func (s *Service) beginDiagnosticTaskStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep) error {
	now := time.Now().UTC()
	task.Status = DiagnosticTaskStatusRunning
	task.CurrentStep = step.Code
	task.UpdatedAt = now
	if task.StartedAt == nil {
		task.StartedAt = &now
	}
	if err := s.UpdateDiagnosticTask(ctx, task); err != nil {
		return err
	}
	step.Status = DiagnosticTaskStatusRunning
	step.StartedAt = &now
	step.CompletedAt = nil
	step.Message = step.Description
	return s.UpdateDiagnosticTaskStep(ctx, step)
}

func (s *Service) finishDiagnosticTaskStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, message string) error {
	now := time.Now().UTC()
	step.Status = DiagnosticTaskStatusSucceeded
	step.Message = message
	step.Error = ""
	step.CompletedAt = &now
	return s.UpdateDiagnosticTaskStep(ctx, step)
}

func (s *Service) failDiagnosticTaskStep(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, stepErr error) error {
	now := time.Now().UTC()
	step.Status = DiagnosticTaskStatusFailed
	step.Error = stepErr.Error()
	step.Message = stepErr.Error()
	step.CompletedAt = &now
	return s.UpdateDiagnosticTaskStep(ctx, step)
}

func (s *Service) failDiagnosticTask(ctx context.Context, task *DiagnosticTask, failureStep DiagnosticStepCode, taskErr error) error {
	now := time.Now().UTC()
	task.Status = DiagnosticTaskStatusFailed
	task.FailureStep = failureStep
	task.FailureReason = taskErr.Error()
	task.CompletedAt = &now
	task.UpdatedAt = now
	return s.UpdateDiagnosticTask(ctx, task)
}

func (s *Service) beginDiagnosticNodeStep(ctx context.Context, step *DiagnosticTaskStep, node *DiagnosticNodeExecution, message string) error {
	now := time.Now().UTC()
	node.TaskStepID = uintPtr(step.ID)
	node.CurrentStep = step.Code
	node.Status = DiagnosticTaskStatusRunning
	node.Message = message
	node.Error = ""
	node.StartedAt = &now
	return s.UpdateDiagnosticNodeExecution(ctx, node)
}

func (s *Service) finishDiagnosticNodeStep(ctx context.Context, step *DiagnosticTaskStep, node *DiagnosticNodeExecution, status DiagnosticTaskStatus, message string) error {
	now := time.Now().UTC()
	node.TaskStepID = uintPtr(step.ID)
	node.CurrentStep = step.Code
	node.Status = status
	node.Message = message
	if status != DiagnosticTaskStatusFailed {
		node.Error = ""
	}
	node.CompletedAt = &now
	return s.UpdateDiagnosticNodeExecution(ctx, node)
}

func (s *Service) writeDiagnosticJSONArtifact(ctx context.Context, task *DiagnosticTask, step *DiagnosticTaskStep, state *diagnosticBundleExecutionState, bundleDir, fileName string, payload interface{}, artifact *diagnosticBundleArtifact) error {
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(bundleDir, fileName)
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return err
	}
	artifact.Path = path
	artifact.SizeBytes = int64(len(bytes))
	state.Artifacts = append(state.Artifacts, artifact)
	return s.AppendDiagnosticStepLog(ctx, &DiagnosticStepLog{
		TaskID:         task.ID,
		TaskStepID:     uintPtr(step.ID),
		StepCode:       step.Code,
		Level:          DiagnosticLogLevelInfo,
		EventType:      DiagnosticLogEventTypeSuccess,
		Message:        bilingualText(fmt.Sprintf("已生成 %s", fileName), fmt.Sprintf("Created %s", fileName)),
		CommandSummary: path,
		CreatedAt:      time.Now().UTC(),
	})
}

func writeDiagnosticBundleManifestFile(path string, task *DiagnosticTask, artifacts []*diagnosticBundleArtifact, state *diagnosticBundleExecutionState) error {
	manifest := buildDiagnosticBundleManifest(task, artifacts, state)
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func buildDiagnosticBundleManifest(task *DiagnosticTask, artifacts []*diagnosticBundleArtifact, state *diagnosticBundleExecutionState) *diagnosticBundleManifest {
	if task == nil {
		return nil
	}
	window := resolveDiagnosticCollectionWindow(task, nil)
	if state != nil {
		if state.WindowStart != nil && !state.WindowStart.IsZero() {
			window.Start = state.WindowStart.UTC()
		}
		if state.WindowEnd != nil && !state.WindowEnd.IsZero() {
			window.End = state.WindowEnd.UTC()
		}
		if state.LookbackMinutes > 0 {
			window.LookbackMinutes = state.LookbackMinutes
		}
		if state.InspectionDetail != nil {
			window = resolveDiagnosticCollectionWindow(task, state.InspectionDetail)
		}
	}
	return &diagnosticBundleManifest{
		Version:         "v2",
		TaskID:          task.ID,
		ClusterID:       task.ClusterID,
		TriggerSource:   task.TriggerSource,
		SourceRef:       task.SourceRef,
		Options:         task.Options.Normalize(),
		Status:          task.Status,
		Summary:         task.Summary,
		LookbackMinutes: window.LookbackMinutes,
		WindowStart:     timePtr(window.Start),
		WindowEnd:       timePtr(window.End),
		GeneratedAt:     time.Now().UTC(),
		Artifacts:       cloneDiagnosticArtifacts(artifacts),
	}
}

func cloneDiagnosticArtifacts(src []*diagnosticBundleArtifact) []*diagnosticBundleArtifact {
	if len(src) == 0 {
		return []*diagnosticBundleArtifact{}
	}
	result := make([]*diagnosticBundleArtifact, 0, len(src))
	for _, item := range src {
		if item == nil {
			continue
		}
		copyItem := *item
		result = append(result, &copyItem)
	}
	return result
}

func buildDiagnosticBundleHTMLPayload(task *DiagnosticTask, state *diagnosticBundleExecutionState, bundleDir string, artifacts []*diagnosticBundleArtifact) *diagnosticBundleHTMLPayload {
	payload := &diagnosticBundleHTMLPayload{
		GeneratedAt:        time.Now().UTC(),
		Task:               buildDiagnosticBundleHTMLTaskSummary(task),
		SourceTraceability: buildDiagnosticBundleHTMLTraceItems(task),
		TaskExecution:      buildDiagnosticBundleHTMLExecutionPanel(task),
		ArtifactGroups:     buildDiagnosticBundleHTMLArtifactGroups(bundleDir, artifacts),
	}
	payload.Health = buildDiagnosticBundleHTMLHealthSummary(task, state, artifacts)
	if state == nil {
		payload.Recommendations = buildDiagnosticBundleHTMLRecommendations(task, state)
		payload.PassedChecks = buildDiagnosticBundleHTMLPassedChecks(task, state, artifacts)
		return payload
	}
	// Evidence & 附录
	payload.Cluster = buildDiagnosticBundleHTMLClusterSummary(state.ClusterSnapshot)
	payload.Inspection = buildDiagnosticBundleHTMLInspectionPanel(state.InspectionDetail)
	payload.ErrorContext = buildDiagnosticBundleHTMLErrorPanel(state.ErrorGroup, state.ErrorEvents, state.LogSamples)
	payload.AlertSnapshot = buildDiagnosticBundleHTMLAlertPanel(state.AlertSnapshot)
	payload.ProcessEvents = buildDiagnosticBundleHTMLProcessPanel(state.ProcessEvents)
	payload.ConfigSnapshot = buildDiagnosticBundleHTMLConfigPanel(state.ConfigSnapshot)
	payload.MetricsSnapshot = buildDiagnosticBundleHTMLMetricsPanel(state.MetricsSnapshot)
	payload.Recommendations = buildDiagnosticBundleHTMLRecommendations(task, state)
	payload.PassedChecks = buildDiagnosticBundleHTMLPassedChecks(task, state, artifacts)
	return payload
}

func buildDiagnosticBundleHTMLTaskSummary(task *DiagnosticTask) diagnosticBundleHTMLTaskSummary {
	summary := diagnosticBundleHTMLTaskSummary{}
	if task == nil {
		return summary
	}
	selectedNodes := make([]diagnosticBundleHTMLNodeTarget, 0, len(task.SelectedNodes))
	for _, node := range sortedDiagnosticTaskTargets(task.SelectedNodes) {
		selectedNodes = append(selectedNodes, diagnosticBundleHTMLNodeTarget{
			HostLabel:   resolveDiagnosticHostLabel(node.HostName, node.HostID, node.HostIP),
			Role:        normalizeDiagnosticDisplayText(node.Role),
			InstallDir:  normalizeDiagnosticDisplayText(node.InstallDir),
			ClusterNode: fmt.Sprintf("#%d", node.ClusterNodeID),
		})
	}
	return diagnosticBundleHTMLTaskSummary{
		ID:            task.ID,
		Status:        task.Status,
		TriggerSource: task.TriggerSource,
		Summary:       normalizeDiagnosticDisplayText(task.Summary),
		CreatedBy:     firstNonEmptyString(strings.TrimSpace(task.CreatedByName), fmt.Sprintf("%d", task.CreatedBy)),
		StartedAt:     task.StartedAt,
		CompletedAt:   task.CompletedAt,
		BundleDir:     normalizeDiagnosticDisplayText(task.BundleDir),
		ManifestPath:  normalizeDiagnosticDisplayText(task.ManifestPath),
		IndexPath:     normalizeDiagnosticDisplayText(task.IndexPath),
		Options:       task.Options.Normalize(),
		SelectedNodes: selectedNodes,
	}
}

func buildDiagnosticBundleHTMLTraceItems(task *DiagnosticTask) []diagnosticBundleHTMLTraceItem {
	if task == nil {
		return nil
	}
	items := []diagnosticBundleHTMLTraceItem{
		{Label: bilingualText("触发来源", "Trigger Source"), Value: normalizeDiagnosticDisplayText(string(task.TriggerSource))},
	}
	if task.SourceRef.ErrorGroupID > 0 {
		items = append(items, diagnosticBundleHTMLTraceItem{
			Label: bilingualText("错误组", "Error Group"),
			Value: fmt.Sprintf("#%d", task.SourceRef.ErrorGroupID),
		})
	}
	if task.SourceRef.InspectionReportID > 0 {
		items = append(items, diagnosticBundleHTMLTraceItem{
			Label: bilingualText("巡检报告", "Inspection Report"),
			Value: fmt.Sprintf("#%d", task.SourceRef.InspectionReportID),
		})
	}
	if task.SourceRef.InspectionFindingID > 0 {
		items = append(items, diagnosticBundleHTMLTraceItem{
			Label: bilingualText("巡检发现", "Inspection Finding"),
			Value: fmt.Sprintf("#%d", task.SourceRef.InspectionFindingID),
		})
	}
	if alertID := strings.TrimSpace(task.SourceRef.AlertID); alertID != "" {
		items = append(items, diagnosticBundleHTMLTraceItem{
			Label: bilingualText("告警 ID", "Alert ID"),
			Value: alertID,
		})
	}
	return items
}

func buildDiagnosticBundleHTMLClusterSummary(snapshot *cluster.Cluster) *diagnosticBundleHTMLClusterSummary {
	if snapshot == nil {
		return nil
	}
	nodes := make([]diagnosticBundleHTMLClusterNode, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		nodes = append(nodes, diagnosticBundleHTMLClusterNode{
			Role:       normalizeDiagnosticDisplayText(string(node.Role)),
			HostID:     node.HostID,
			InstallDir: normalizeDiagnosticDisplayText(node.InstallDir),
			Status:     normalizeDiagnosticDisplayText(string(node.Status)),
			ProcessPID: node.ProcessPID,
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].HostID != nodes[j].HostID {
			return nodes[i].HostID < nodes[j].HostID
		}
		return nodes[i].Role < nodes[j].Role
	})
	return &diagnosticBundleHTMLClusterSummary{
		ID:             snapshot.ID,
		Name:           normalizeDiagnosticDisplayText(snapshot.Name),
		Version:        normalizeDiagnosticDisplayText(snapshot.Version),
		Status:         normalizeDiagnosticDisplayText(string(snapshot.Status)),
		DeploymentMode: normalizeDiagnosticDisplayText(string(snapshot.DeploymentMode)),
		InstallDir:     normalizeDiagnosticDisplayText(snapshot.InstallDir),
		NodeCount:      len(snapshot.Nodes),
		Nodes:          nodes,
	}
}

func buildDiagnosticBundleHTMLInspectionPanel(detail *ClusterInspectionReportDetailData) *diagnosticBundleHTMLInspectionPanel {
	if detail == nil || detail.Report == nil {
		return nil
	}
	findings := make([]diagnosticBundleHTMLFinding, 0, len(detail.Findings))
	for _, finding := range detail.Findings {
		if finding == nil {
			continue
		}
		findings = append(findings, diagnosticBundleHTMLFinding{
			Severity:       normalizeDiagnosticDisplayText(string(finding.Severity)),
			CheckName:      normalizeDiagnosticDisplayText(firstNonEmptyString(finding.CheckName, finding.CheckCode)),
			CheckCode:      normalizeDiagnosticDisplayText(finding.CheckCode),
			Summary:        normalizeDiagnosticDisplayText(finding.Summary),
			Recommendation: normalizeDiagnosticDisplayText(finding.Recommendation),
			Evidence:       normalizeDiagnosticDisplayText(finding.EvidenceSummary),
		})
	}
	return &diagnosticBundleHTMLInspectionPanel{
		Summary:         normalizeDiagnosticDisplayText(detail.Report.Summary),
		Status:          detail.Report.Status,
		RequestedBy:     normalizeDiagnosticDisplayText(detail.Report.RequestedBy),
		LookbackMinutes: firstNonZeroInt(detail.Report.LookbackMinutes, defaultInspectionLookbackMinutes),
		CriticalCount:   detail.Report.CriticalCount,
		WarningCount:    detail.Report.WarningCount,
		InfoCount:       detail.Report.InfoCount,
		StartedAt:       detail.Report.StartedAt,
		FinishedAt:      detail.Report.FinishedAt,
		Findings:        findings,
	}
}

func buildDiagnosticBundleHTMLErrorPanel(group *SeatunnelErrorGroup, events []*SeatunnelErrorEvent, logSamples []diagnosticCollectedLogSample) *diagnosticBundleHTMLErrorPanel {
	if group == nil && len(events) == 0 && len(logSamples) == 0 {
		return nil
	}
	panel := &diagnosticBundleHTMLErrorPanel{
		Events:     make([]diagnosticBundleHTMLErrorEvent, 0, len(events)),
		LogSamples: make([]diagnosticBundleHTMLLogSample, 0, len(logSamples)),
	}
	if group != nil {
		panel.GroupTitle = normalizeDiagnosticDisplayText(group.Title)
		panel.ExceptionClass = normalizeDiagnosticDisplayText(group.ExceptionClass)
		panel.OccurrenceCount = group.OccurrenceCount
		panel.FirstSeenAt = &group.FirstSeenAt
		panel.LastSeenAt = &group.LastSeenAt
		panel.SampleMessage = normalizeDiagnosticDisplayText(group.SampleMessage)
	}
	sortedEvents := make([]*SeatunnelErrorEvent, 0, len(events))
	sortedEvents = append(sortedEvents, events...)
	sort.SliceStable(sortedEvents, func(i, j int) bool {
		left := sortedEvents[i]
		right := sortedEvents[j]
		if left == nil || right == nil {
			return left != nil
		}
		if !left.OccurredAt.Equal(right.OccurredAt) {
			return left.OccurredAt.Before(right.OccurredAt)
		}
		return left.ID < right.ID
	})
	for _, event := range sortedEvents {
		if event == nil {
			continue
		}
		panel.Events = append(panel.Events, diagnosticBundleHTMLErrorEvent{
			OccurredAt: formatDiagnosticBundleTimeValue(event.OccurredAt),
			Role:       normalizeDiagnosticDisplayText(event.Role),
			HostLabel:  resolveDiagnosticHostLabel("", event.HostID, ""),
			SourceFile: normalizeDiagnosticDisplayText(event.SourceFile),
			JobID:      normalizeDiagnosticDisplayText(event.JobID),
			Message:    normalizeDiagnosticDisplayText(event.Message),
			Evidence:   normalizeDiagnosticDisplayText(event.Evidence),
		})
	}
	panel.RecentEventCount = len(panel.Events)
	for _, item := range logSamples {
		panel.LogSamples = append(panel.LogSamples, diagnosticBundleHTMLLogSample{
			HostLabel:   resolveDiagnosticHostLabel(item.HostName, item.HostID, item.HostIP),
			SourceFile:  normalizeDiagnosticDisplayText(item.SourceFile),
			WindowLabel: fmt.Sprintf("%s ~ %s", formatDiagnosticBundleTimeValue(item.WindowStart), formatDiagnosticBundleTimeValue(item.WindowEnd)),
			Content:     normalizeDiagnosticDisplayText(item.Content),
		})
	}
	return panel
}

func buildDiagnosticBundleHTMLAlertPanel(alerts []*monitoringapp.AlertInstance) *diagnosticBundleHTMLAlertPanel {
	if len(alerts) == 0 {
		return nil
	}
	panel := &diagnosticBundleHTMLAlertPanel{
		Alerts: make([]diagnosticBundleHTMLAlertItem, 0, len(alerts)),
	}
	for _, alert := range alerts {
		if alert == nil {
			continue
		}
		item := diagnosticBundleHTMLAlertItem{
			Name:        normalizeDiagnosticDisplayText(alert.AlertName),
			Severity:    normalizeDiagnosticDisplayText(string(alert.Severity)),
			Status:      normalizeDiagnosticDisplayText(string(alert.Status)),
			CreatedAt:   formatDiagnosticBundleTimeValue(alert.CreatedAt),
			FiringAt:    formatDiagnosticBundleTimeValue(alert.FiringAt),
			LastSeenAt:  formatDiagnosticBundleTimeValue(alert.LastSeenAt),
			ResolvedAt:  formatDiagnosticBundleTime(alert.ResolvedAt),
			Summary:     normalizeDiagnosticDisplayText(alert.Summary),
			Description: normalizeDiagnosticDisplayText(alert.Description),
		}
		switch alert.Severity {
		case monitoringapp.AlertSeverityCritical:
			panel.Critical++
		default:
			panel.Warning++
		}
		if alert.Status == monitoringapp.AlertDisplayStatusFiring {
			panel.Firing++
		}
		if panel.FirstSeenAt == "" || (!alert.CreatedAt.IsZero() && formatDiagnosticBundleTimeValue(alert.CreatedAt) < panel.FirstSeenAt) {
			panel.FirstSeenAt = formatDiagnosticBundleTimeValue(alert.CreatedAt)
		}
		lastSeen := formatDiagnosticBundleTimeValue(alert.LastSeenAt)
		if panel.LastSeenAt == "" || lastSeen > panel.LastSeenAt {
			panel.LastSeenAt = lastSeen
		}
		panel.Alerts = append(panel.Alerts, item)
	}
	panel.Total = len(panel.Alerts)
	return panel
}

func buildDiagnosticBundleHTMLProcessPanel(events []*monitor.ProcessEvent) *diagnosticBundleHTMLProcessPanel {
	if len(events) == 0 {
		return nil
	}
	panel := &diagnosticBundleHTMLProcessPanel{
		Total:  len(events),
		Events: make([]diagnosticBundleHTMLProcessEvent, 0, len(events)),
	}
	typeCounter := make(map[string]int)
	for _, event := range events {
		if event == nil {
			continue
		}
		eventType := normalizeDiagnosticDisplayText(string(event.EventType))
		typeCounter[eventType]++
		panel.Events = append(panel.Events, diagnosticBundleHTMLProcessEvent{
			CreatedAt:   formatDiagnosticBundleTimeValue(event.CreatedAt),
			EventType:   eventType,
			ProcessName: normalizeDiagnosticDisplayText(event.ProcessName),
			NodeLabel:   resolveDiagnosticHostLabel("", event.HostID, fmt.Sprintf("node-%d", event.NodeID)),
			Details:     normalizeDiagnosticDisplayText(event.Details),
		})
	}
	typeKeys := make([]string, 0, len(typeCounter))
	for key := range typeCounter {
		typeKeys = append(typeKeys, key)
	}
	sort.Strings(typeKeys)
	panel.ByType = make([]diagnosticBundleHTMLMetricCard, 0, len(typeKeys))
	for _, key := range typeKeys {
		panel.ByType = append(panel.ByType, diagnosticBundleHTMLMetricCard{
			Label: key,
			Value: fmt.Sprintf("%d", typeCounter[key]),
		})
	}
	return panel
}

func buildDiagnosticBundleHTMLConfigPanel(summary *diagnosticConfigSnapshotSummary) *diagnosticBundleHTMLConfigPanel {
	if summary == nil {
		return nil
	}
	recentChanges := append([]diagnosticConfigChangeRecord(nil), summary.ConfigChanges...)
	remainingChanges := []diagnosticConfigChangeRecord{}
	if len(recentChanges) > 8 {
		remainingChanges = append(remainingChanges, recentChanges[8:]...)
		recentChanges = recentChanges[:8]
	}
	return &diagnosticBundleHTMLConfigPanel{
		FileCount:          len(summary.Files),
		KeyHighlightCount:  len(summary.KeyHighlights),
		DirectoryCount:     len(summary.DirectoryManifests),
		ChangedConfigCount: len(summary.ConfigChanges),
		KeyHighlights:      append([]diagnosticConfigKeyHighlight(nil), summary.KeyHighlights...),
		FilePreviews:       append([]diagnosticConfigFilePreview(nil), summary.FilePreviews...),
		RecentChanges:      recentChanges,
		RemainingChanges:   remainingChanges,
		Files:              summary.Files,
		DirectoryManifests: summary.DirectoryManifests,
		ConfigChanges:      summary.ConfigChanges,
		CollectionNotes:    summary.CollectionNotes,
	}
}

func buildDiagnosticBundleHTMLMetricsPanel(snapshot *diagnosticPrometheusSnapshot) *diagnosticBundleHTMLMetricsPanel {
	if snapshot == nil || (len(snapshot.Signals) == 0 && len(snapshot.CollectionNotes) == 0) {
		return nil
	}
	highlighted := make([]diagnosticPrometheusSignal, 0, len(snapshot.Signals))
	additional := make([]diagnosticPrometheusSignal, 0, len(snapshot.Signals))
	anomalyCount := 0
	for _, signal := range snapshot.Signals {
		if strings.EqualFold(strings.TrimSpace(signal.Status), "healthy") || strings.TrimSpace(signal.Status) == "" {
			additional = append(additional, signal)
			continue
		}
		anomalyCount++
		highlighted = append(highlighted, signal)
	}
	if len(highlighted) == 0 {
		highlighted = append(highlighted, snapshot.Signals...)
		if len(highlighted) > 3 {
			additional = append([]diagnosticPrometheusSignal(nil), highlighted[3:]...)
			highlighted = append([]diagnosticPrometheusSignal(nil), highlighted[:3]...)
		} else {
			additional = []diagnosticPrometheusSignal{}
		}
	}
	return &diagnosticBundleHTMLMetricsPanel{
		SignalCount:        len(snapshot.Signals),
		AnomalyCount:       anomalyCount,
		HighlightedSignals: highlighted,
		AdditionalSignals:  additional,
		CollectionNotes:    append([]string(nil), snapshot.CollectionNotes...),
	}
}

func buildDiagnosticBundleHTMLExecutionPanel(task *DiagnosticTask) diagnosticBundleHTMLExecutionPanel {
	panel := diagnosticBundleHTMLExecutionPanel{
		Steps: []diagnosticBundleHTMLExecutionStep{},
		Nodes: []diagnosticBundleHTMLExecutionNode{},
	}
	if task == nil {
		return panel
	}
	steps := make([]DiagnosticTaskStep, 0, len(task.Steps))
	steps = append(steps, task.Steps...)
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].Sequence != steps[j].Sequence {
			return steps[i].Sequence < steps[j].Sequence
		}
		return steps[i].ID < steps[j].ID
	})
	for _, step := range steps {
		panel.Steps = append(panel.Steps, diagnosticBundleHTMLExecutionStep{
			Sequence:    step.Sequence,
			Code:        string(step.Code),
			Title:       normalizeDiagnosticDisplayText(step.Title),
			Status:      normalizeDiagnosticDisplayText(string(step.Status)),
			Message:     normalizeDiagnosticDisplayText(step.Message),
			Error:       normalizeDiagnosticDisplayText(step.Error),
			StartedAt:   formatDiagnosticBundleTime(step.StartedAt),
			CompletedAt: formatDiagnosticBundleTime(step.CompletedAt),
		})
	}
	nodes := make([]DiagnosticNodeExecution, 0, len(task.NodeExecutions))
	nodes = append(nodes, task.NodeExecutions...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].HostID != nodes[j].HostID {
			return nodes[i].HostID < nodes[j].HostID
		}
		return nodes[i].Role < nodes[j].Role
	})
	for _, node := range nodes {
		panel.Nodes = append(panel.Nodes, diagnosticBundleHTMLExecutionNode{
			HostLabel:   resolveDiagnosticHostLabel(node.HostName, node.HostID, node.HostIP),
			Role:        normalizeDiagnosticDisplayText(node.Role),
			Status:      normalizeDiagnosticDisplayText(string(node.Status)),
			CurrentStep: normalizeDiagnosticDisplayText(string(node.CurrentStep)),
			Message:     normalizeDiagnosticDisplayText(node.Message),
			Error:       normalizeDiagnosticDisplayText(node.Error),
			StartedAt:   formatDiagnosticBundleTime(node.StartedAt),
			CompletedAt: formatDiagnosticBundleTime(node.CompletedAt),
		})
	}
	return panel
}

func buildDiagnosticBundleHTMLHealthSummary(task *DiagnosticTask, state *diagnosticBundleExecutionState, artifacts []*diagnosticBundleArtifact) diagnosticBundleHTMLHealthSummary {
	summary := diagnosticBundleHTMLHealthSummary{
		Tone:    "neutral",
		Title:   "",
		Summary: "",
		Metrics: []diagnosticBundleHTMLMetricCard{},
	}

	if state != nil && state.InspectionDetail != nil && state.InspectionDetail.Report != nil {
		report := state.InspectionDetail.Report
		// 按巡检结果给出一句话结论与语气
		switch {
		case report.Status == InspectionReportStatusFailed || report.CriticalCount > 0:
			summary.Tone = "critical"
			summary.Title = bilingualText("集群存在严重风险", "Cluster requires immediate attention")
			summary.Summary = normalizeDiagnosticDisplayText(firstNonEmptyString(report.Summary, report.ErrorMessage))
		case report.WarningCount > 0:
			summary.Tone = "warning"
			summary.Title = bilingualText("集群存在待排查问题", "Cluster has issues to investigate")
			summary.Summary = normalizeDiagnosticDisplayText(report.Summary)
		default:
			summary.Tone = "healthy"
			summary.Title = bilingualText("巡检未发现明显异常", "Inspection found no critical issue")
			summary.Summary = normalizeDiagnosticDisplayText(report.Summary)
		}
		// 仅保留与“时间范围 + 发现数”直接相关的少量指标
		windowLabel := fmt.Sprintf("%d min", firstNonZeroInt(report.LookbackMinutes, defaultInspectionLookbackMinutes))
		windowNote := ""
		if state.WindowStart != nil && state.WindowEnd != nil {
			windowNote = bilingualText(
				fmt.Sprintf("%s ~ %s", formatDiagnosticBundleTime(state.WindowStart), formatDiagnosticBundleTime(state.WindowEnd)),
				fmt.Sprintf("%s ~ %s", formatDiagnosticBundleTime(state.WindowStart), formatDiagnosticBundleTime(state.WindowEnd)),
			)
		}
		summary.Metrics = append(summary.Metrics,
			diagnosticBundleHTMLMetricCard{
				Label: bilingualText("时间范围", "Time Window"),
				Value: windowLabel,
				Note:  windowNote,
			},
			diagnosticBundleHTMLMetricCard{
				Label: bilingualText("发现统计", "Findings"),
				Value: fmt.Sprintf("%d", report.FindingTotal),
				Note: bilingualText(
					fmt.Sprintf("严重 %d / 告警 %d / 信息 %d", report.CriticalCount, report.WarningCount, report.InfoCount),
					fmt.Sprintf("critical %d / warning %d / info %d", report.CriticalCount, report.WarningCount, report.InfoCount),
				),
			},
		)
		if state.MetricsSnapshot != nil {
			anomalyCount := 0
			for _, signal := range state.MetricsSnapshot.Signals {
				if strings.TrimSpace(signal.Status) != "" && !strings.EqualFold(signal.Status, "healthy") {
					anomalyCount++
				}
			}
			summary.Metrics = append(summary.Metrics, diagnosticBundleHTMLMetricCard{
				Label: bilingualText("指标信号", "Metric Signals"),
				Value: fmt.Sprintf("%d", len(state.MetricsSnapshot.Signals)),
				Note: bilingualText(
					fmt.Sprintf("异常信号 %d", anomalyCount),
					fmt.Sprintf("%d anomalous signal(s)", anomalyCount),
				),
			})
		}
	} else {
		// 无巡检上下文时，仅给出非常简短的概览
		clusterLabel := "-"
		switch {
		case state != nil && state.ClusterSnapshot != nil && strings.TrimSpace(state.ClusterSnapshot.Name) != "":
			clusterLabel = state.ClusterSnapshot.Name
		case task != nil && task.ClusterID > 0:
			clusterLabel = fmt.Sprintf("#%d", task.ClusterID)
		}
		window := resolveDiagnosticCollectionWindow(task, nil)
		if state != nil {
			if state.WindowStart != nil && state.WindowEnd != nil {
				window.Start = state.WindowStart.UTC()
				window.End = state.WindowEnd.UTC()
			}
			if state.LookbackMinutes > 0 {
				window.LookbackMinutes = state.LookbackMinutes
			}
		}
		summary.Title = bilingualText("诊断报告已生成", "Diagnostic report generated")
		summary.Summary = bilingualText(
			"当前报告基于错误、告警与运行时信号生成，可结合下方证据详情排查问题。",
			"This report aggregates errors, alerts and runtime signals for troubleshooting.",
		)
		summary.Metrics = append(summary.Metrics,
			diagnosticBundleHTMLMetricCard{
				Label: bilingualText("集群", "Cluster"),
				Value: clusterLabel,
				Note:  "",
			},
			diagnosticBundleHTMLMetricCard{
				Label: bilingualText("时间范围", "Time Window"),
				Value: fmt.Sprintf("%d min", window.LookbackMinutes),
				Note: bilingualText(
					fmt.Sprintf("%s ~ %s", formatDiagnosticBundleTimeValue(window.Start), formatDiagnosticBundleTimeValue(window.End)),
					fmt.Sprintf("%s ~ %s", formatDiagnosticBundleTimeValue(window.Start), formatDiagnosticBundleTimeValue(window.End)),
				),
			},
		)
		if state != nil && state.MetricsSnapshot != nil {
			anomalyCount := 0
			for _, signal := range state.MetricsSnapshot.Signals {
				if strings.TrimSpace(signal.Status) != "" && !strings.EqualFold(signal.Status, "healthy") {
					anomalyCount++
				}
			}
			summary.Metrics = append(summary.Metrics, diagnosticBundleHTMLMetricCard{
				Label: bilingualText("指标信号", "Metric Signals"),
				Value: fmt.Sprintf("%d", len(state.MetricsSnapshot.Signals)),
				Note: bilingualText(
					fmt.Sprintf("异常信号 %d", anomalyCount),
					fmt.Sprintf("%d anomalous signal(s)", anomalyCount),
				),
			})
		}
	}
	return summary
}

func buildDiagnosticBundleHTMLArtifactGroups(bundleDir string, artifacts []*diagnosticBundleArtifact) []diagnosticBundleHTMLArtifactGroup {
	if len(artifacts) == 0 {
		return []diagnosticBundleHTMLArtifactGroup{}
	}
	groupMap := make(map[string][]diagnosticBundleHTMLArtifactView)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		view := buildDiagnosticBundleHTMLArtifactView(bundleDir, artifact)
		groupMap[artifact.Category] = append(groupMap[artifact.Category], view)
	}
	order := []string{
		"error_context",
		"metrics_snapshot",
		"config_snapshot",
		"directory_inventory",
		"alert_snapshot",
		"process_events",
		"log_sample",
		"thread_dump",
		"jvm_dump",
		"manifest",
		"html_summary",
		"diagnostic_report",
	}
	seen := make(map[string]struct{}, len(order))
	result := make([]diagnosticBundleHTMLArtifactGroup, 0, len(groupMap))
	for _, key := range order {
		items, ok := groupMap[key]
		if !ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, diagnosticBundleHTMLArtifactGroup{
			Key:   key,
			Label: resolveDiagnosticArtifactCategoryLabel(key),
			Items: items,
		})
	}
	remaining := make([]string, 0)
	for key := range groupMap {
		if _, ok := seen[key]; ok {
			continue
		}
		remaining = append(remaining, key)
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		result = append(result, diagnosticBundleHTMLArtifactGroup{
			Key:   key,
			Label: resolveDiagnosticArtifactCategoryLabel(key),
			Items: groupMap[key],
		})
	}
	return result
}

func buildDiagnosticBundleHTMLRecommendations(task *DiagnosticTask, state *diagnosticBundleExecutionState) []diagnosticBundleHTMLAdvice {
	items := make([]diagnosticBundleHTMLAdvice, 0, 3)
	appendAdvice := func(title, details string) {
		title = strings.TrimSpace(title)
		details = strings.TrimSpace(details)
		if title == "" && details == "" {
			return
		}
		items = append(items, diagnosticBundleHTMLAdvice{
			Title:   normalizeDiagnosticDisplayText(title),
			Details: normalizeDiagnosticDisplayText(details),
		})
	}

	// 优先从巡检发现中挑选 1~2 条最关键建议
	if state != nil && state.InspectionDetail != nil {
		for _, finding := range state.InspectionDetail.Findings {
			if finding == nil {
				continue
			}
			appendAdvice(
				firstNonEmptyString(finding.CheckName, finding.Summary),
				firstNonEmptyString(finding.Recommendation, finding.EvidenceSummary),
			)
			if len(items) >= 2 {
				break
			}
		}
	}
	// 兜底：针对错误组或告警给出 1 条高层动作建议
	if len(items) == 0 && state != nil && state.ErrorGroup != nil {
		appendAdvice(
			bilingualText("优先排查错误组根因", "Prioritize the root cause of the error group"),
			firstNonEmptyString(state.ErrorGroup.SampleMessage, state.ErrorGroup.Title),
		)
	}
	if len(items) == 0 && state != nil && len(state.AlertSnapshot) > 0 {
		alert := state.AlertSnapshot[0]
		if alert != nil {
			appendAdvice(
				bilingualText("先处理活动告警", "Address active alerts first"),
				firstNonEmptyString(alert.Summary, alert.Description, alert.AlertName),
			)
		}
	}
	// 不再强行追加通用长段落，仅在确有 JVM Dump 时补充一条提示
	if task != nil && task.Options.IncludeJVMDump {
		appendAdvice(
			bilingualText("复查 JVM Dump 元数据", "Review JVM dump metadata"),
			bilingualText("当前 MVP 仅登记 JVM Dump 远端路径与元数据，如需 hprof 二进制回传请走后续增强能力。", "The MVP records JVM dump metadata and remote paths only. Binary HPROF upload is deferred to a later enhancement."),
		)
	}
	return items
}

func buildDiagnosticBundleHTMLPassedChecks(task *DiagnosticTask, state *diagnosticBundleExecutionState, artifacts []*diagnosticBundleArtifact) []diagnosticBundleHTMLAdvice {
	items := make([]diagnosticBundleHTMLAdvice, 0, 4)
	appendAdvice := func(title, details string) {
		title = strings.TrimSpace(title)
		details = strings.TrimSpace(details)
		if title == "" && details == "" {
			return
		}
		items = append(items, diagnosticBundleHTMLAdvice{
			Title:   normalizeDiagnosticDisplayText(title),
			Details: normalizeDiagnosticDisplayText(details),
		})
	}

	artifactCountByCategory := make(map[string]int)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		artifactCountByCategory[strings.TrimSpace(artifact.Category)]++
	}
	if artifactCountByCategory["config_snapshot"] > 0 {
		appendAdvice(
			bilingualText("运行配置文件已采集", "Runtime config files collected"),
			bilingualText("报告已附带 seatunnel.yaml、hazelcast 配置等运行配置文件快照。", "The report contains runtime config snapshots such as seatunnel.yaml and Hazelcast configs."),
		)
	}
	if artifactCountByCategory["log_sample"] > 0 {
		appendAdvice(
			bilingualText("日志样本已落盘", "Log sample collected"),
			bilingualText("至少一个节点已采集到用于排查的日志样本。", "At least one node produced a log sample for investigation."),
		)
	}
	if artifactCountByCategory["thread_dump"] > 0 {
		appendAdvice(
			bilingualText("线程栈已采集", "Thread dump collected"),
			bilingualText("可直接打开线程栈产物定位阻塞、卡死或线程热点。", "Thread dump artifacts are available for blocking and hotspot analysis."),
		)
	}
	if task != nil && !task.Options.IncludeJVMDump {
		appendAdvice(
			bilingualText("JVM Dump 按策略跳过", "JVM dump intentionally skipped"),
			bilingualText("当前任务未开启 JVM Dump，不视为任务失败。", "JVM dump was disabled by task option and is not treated as a failure."),
		)
	}
	if state != nil && state.InspectionDetail != nil && state.InspectionDetail.Report != nil && state.InspectionDetail.Report.CriticalCount == 0 {
		appendAdvice(
			bilingualText("巡检未发现严重问题", "No critical inspection findings"),
			bilingualText("本次巡检上下文中没有 critical 级别发现项。", "The inspection context contains no critical findings."),
		)
	}
	if state != nil && len(state.AlertSnapshot) == 0 {
		appendAdvice(
			bilingualText("未检测到活动告警", "No active alerts detected"),
			bilingualText("告警快照为空，说明当前上下文没有关联的 firing 告警。", "The alert snapshot is empty, which means no firing alerts were associated with this context."),
		)
	}
	return items
}

func buildDiagnosticBundleHTMLArtifactView(bundleDir string, artifact *diagnosticBundleArtifact) diagnosticBundleHTMLArtifactView {
	relativePath := resolveDiagnosticBundleRelativePath(bundleDir, artifact.Path)
	preview, previewNote := readDiagnosticArtifactPreview(artifact)
	return diagnosticBundleHTMLArtifactView{
		Category:      artifact.Category,
		CategoryLabel: resolveDiagnosticArtifactCategoryLabel(artifact.Category),
		StepCode:      normalizeDiagnosticDisplayText(string(artifact.StepCode)),
		Status:        normalizeDiagnosticDisplayText(artifact.Status),
		Format:        normalizeDiagnosticDisplayText(artifact.Format),
		HostLabel:     resolveDiagnosticHostLabel(artifact.HostName, artifact.HostID, ""),
		LocalPath:     normalizeDiagnosticDisplayText(artifact.Path),
		RelativePath:  normalizeDiagnosticDisplayText(relativePath),
		RemotePath:    normalizeDiagnosticDisplayText(artifact.RemotePath),
		SizeLabel:     formatDiagnosticBytes(artifact.SizeBytes),
		Message:       normalizeDiagnosticDisplayText(artifact.Message),
		Preview:       preview,
		PreviewNote:   previewNote,
	}
}

func resolveDiagnosticArtifactCategoryLabel(category string) string {
	switch strings.TrimSpace(category) {
	case "error_context":
		return bilingualText("错误上下文", "Error Context")
	case "process_events":
		return bilingualText("进程事件", "Process Events")
	case "alert_snapshot":
		return bilingualText("告警快照", "Alert Snapshot")
	case "config_snapshot":
		return bilingualText("运行配置文件", "Runtime Config Files")
	case "directory_inventory":
		return bilingualText("目录清单", "Directory Inventory")
	case "metrics_snapshot":
		return bilingualText("指标快照", "Metrics Snapshot")
	case "log_sample":
		return bilingualText("日志样本", "Log Sample")
	case "thread_dump":
		return bilingualText("线程栈", "Thread Dump")
	case "jvm_dump":
		return bilingualText("JVM Dump", "JVM Dump")
	case "manifest":
		return bilingualText("Manifest", "Manifest")
	case "html_summary":
		return bilingualText("诊断报告", "Diagnostic Report")
	case "diagnostic_report":
		return bilingualText("诊断报告", "Diagnostic Report")
	default:
		return normalizeDiagnosticDisplayText(category)
	}
}

func resolveDiagnosticBundleRelativePath(bundleDir, path string) string {
	trimmedBundle := strings.TrimSpace(bundleDir)
	trimmedPath := strings.TrimSpace(path)
	if trimmedBundle == "" || trimmedPath == "" {
		return ""
	}
	relative, err := filepath.Rel(trimmedBundle, trimmedPath)
	if err != nil {
		return trimmedPath
	}
	if strings.HasPrefix(relative, "..") {
		return trimmedPath
	}
	return filepath.ToSlash(relative)
}

func readDiagnosticArtifactPreview(artifact *diagnosticBundleArtifact) (string, string) {
	if artifact == nil {
		return "", ""
	}
	category := strings.TrimSpace(artifact.Category)
	if category == "diagnostic_report" || category == "html_summary" {
		return "", bilingualText("当前文件即离线诊断报告首页。", "This file is the offline diagnostic report itself.")
	}
	path := strings.TrimSpace(artifact.Path)
	if path == "" {
		return "", ""
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", bilingualText("产物预览读取失败。", "Failed to read artifact preview.")
	}
	content := string(bytes)
	if strings.TrimSpace(content) == "" {
		return "", bilingualText("产物文件为空。", "Artifact file is empty.")
	}
	const maxPreviewRunes = 6000
	preview, truncated := truncateDiagnosticText(content, maxPreviewRunes)
	if truncated {
		return preview, bilingualText("仅展示前 6000 个字符，完整内容请打开对应文件。", "Preview shows the first 6000 characters only. Open the file for full content.")
	}
	return preview, bilingualText("已展示完整产物内容。", "Showing full artifact content.")
}

func truncateDiagnosticText(value string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return "", false
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value, false
	}
	return string(runes[:maxRunes]), true
}

func resolveDiagnosticHostLabel(hostName string, hostID uint, fallback string) string {
	if trimmed := strings.TrimSpace(hostName); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return trimmed
	}
	if hostID > 0 {
		return fmt.Sprintf("#%d", hostID)
	}
	return "-"
}

func normalizeDiagnosticDisplayText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	return trimmed
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func formatDiagnosticBundleTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return formatDiagnosticBundleTimeValue(*value)
}

func formatDiagnosticBundleTimeValue(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.In(diagnosticDisplayLocation()).Format("2006-01-02 15:04:05 MST")
}

func diagnosticDisplayLocation() *time.Location {
	diagnosticDisplayTimezoneOnce.Do(func() {
		name := strings.TrimSpace(os.Getenv("STX_REPORT_TIMEZONE"))
		if name == "" {
			name = "Asia/Shanghai"
		}
		loc, err := time.LoadLocation(name)
		if err != nil {
			diagnosticDisplayTimezone = time.FixedZone("CST", 8*3600)
			return
		}
		diagnosticDisplayTimezone = loc
	})
	if diagnosticDisplayTimezone == nil {
		return time.FixedZone("CST", 8*3600)
	}
	return diagnosticDisplayTimezone
}

func (s *Service) sendDiagnosticAgentCommandWithRetry(ctx context.Context, agentID, command string, params map[string]string) (bool, string, error) {
	if s == nil || s.agentSender == nil {
		return false, "", fmt.Errorf("agent sender is unavailable")
	}
	var (
		success bool
		output  string
		err     error
	)
	for attempt := 0; attempt < 8; attempt++ {
		success, output, err = s.agentSender.SendCommand(ctx, agentID, command, params)
		if !shouldRetryDiagnosticAgentCommand(err, output) {
			return success, output, err
		}
		select {
		case <-ctx.Done():
			return success, output, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return success, output, err
}

func shouldRetryDiagnosticAgentCommand(err error, output string) bool {
	combined := strings.ToLower(strings.TrimSpace(firstNonEmptyString(output, fmt.Sprint(err))))
	if combined == "" {
		return false
	}
	return strings.Contains(combined, "command stream not available") ||
		strings.Contains(combined, "agent not found") ||
		strings.Contains(combined, "agent command stream not found") ||
		strings.Contains(combined, "no active command stream")
}

func formatDiagnosticBytes(size int64) string {
	if size <= 0 {
		return "-"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value = value / 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatDiagnosticMetricValue(unit string, value float64) string {
	switch strings.TrimSpace(unit) {
	case "ratio":
		return fmt.Sprintf("%.1f%%", value*100)
	case "percent":
		return fmt.Sprintf("%.1f%%", value)
	case "cores":
		return fmt.Sprintf("%.2f", value)
	case "bool":
		if value >= 1 {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%.2f", value)
	}
}

func renderDiagnosticMetricChart(points []diagnosticPrometheusPoint, threshold float64, comparator string, unit string) template.HTML {
	if len(points) < 2 {
		return ""
	}
	const width = 360.0
	const height = 156.0
	const leftPadding = 48.0
	const rightPadding = 16.0
	const topPadding = 12.0
	const bottomPadding = 32.0

	minValue := points[0].Value
	maxValue := points[0].Value
	for _, point := range points[1:] {
		if point.Value < minValue {
			minValue = point.Value
		}
		if point.Value > maxValue {
			maxValue = point.Value
		}
	}
	if threshold < minValue {
		minValue = threshold
	}
	if threshold > maxValue {
		maxValue = threshold
	}
	if minValue == maxValue {
		maxValue = minValue + 1
	}

	start := points[0].Timestamp.Unix()
	end := points[len(points)-1].Timestamp.Unix()
	if end <= start {
		end = start + 1
	}
	plotWidth := width - leftPadding - rightPadding
	plotHeight := height - topPadding - bottomPadding
	pathParts := make([]string, 0, len(points))
	for index, point := range points {
		x := leftPadding + (float64(point.Timestamp.Unix()-start)/float64(end-start))*plotWidth
		y := topPadding + (1-((point.Value-minValue)/(maxValue-minValue)))*plotHeight
		if index == 0 {
			pathParts = append(pathParts, fmt.Sprintf("M %.2f %.2f", x, y))
			continue
		}
		pathParts = append(pathParts, fmt.Sprintf("L %.2f %.2f", x, y))
	}
	thresholdY := topPadding + (1-((threshold-minValue)/(maxValue-minValue)))*plotHeight
	if thresholdY < topPadding {
		thresholdY = topPadding
	}
	if thresholdY > height-bottomPadding {
		thresholdY = height - bottomPadding
	}
	strokeColor := "#2563eb"
	if strings.TrimSpace(comparator) == "lt" {
		strokeColor = "#dc2626"
	}
	axisColor := "#94a3b8"
	gridColor := "#e2e8f0"
	lastX := leftPadding + (float64(points[len(points)-1].Timestamp.Unix()-start)/float64(end-start))*plotWidth
	lastY := topPadding + (1-((points[len(points)-1].Value-minValue)/(maxValue-minValue)))*plotHeight
	midTime := points[0].Timestamp.Add(points[len(points)-1].Timestamp.Sub(points[0].Timestamp) / 2)
	formatLabel := func(ts time.Time) string {
		return ts.Local().Format("01-02 15:04")
	}
	yTop := formatDiagnosticMetricValue(unit, maxValue)
	yBottom := formatDiagnosticMetricValue(unit, minValue)
	thresholdLabel := fmt.Sprintf("Threshold %s", formatDiagnosticMetricValue(unit, threshold))
	svg := fmt.Sprintf(
		`<svg viewBox="0 0 %.0f %.0f" class="metric-chart" preserveAspectRatio="none" aria-label="diagnostic metric chart">
<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" class="metric-chart-axis" style="stroke:%s"/>
<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" class="metric-chart-axis" style="stroke:%s"/>
<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" class="metric-chart-grid" style="stroke:%s"/>
<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" class="metric-chart-threshold"/>
<path d="%s" class="metric-chart-path" style="stroke:%s"/>
<circle cx="%.2f" cy="%.2f" r="3.2" class="metric-chart-dot"/>
<text x="%.2f" y="%.2f" class="metric-chart-label y-top">%s</text>
<text x="%.2f" y="%.2f" class="metric-chart-label y-bottom">%s</text>
<text x="%.2f" y="%.2f" class="metric-chart-label x-start">%s</text>
<text x="%.2f" y="%.2f" class="metric-chart-label x-mid">%s</text>
<text x="%.2f" y="%.2f" class="metric-chart-label x-end">%s</text>
<text x="%.2f" y="%.2f" class="metric-chart-label threshold-label">%s</text>
</svg>`,
		width, height,
		leftPadding, topPadding, leftPadding, height-bottomPadding, axisColor,
		leftPadding, height-bottomPadding, width-rightPadding, height-bottomPadding, axisColor,
		leftPadding, topPadding+plotHeight/2, width-rightPadding, topPadding+plotHeight/2, gridColor,
		leftPadding, thresholdY, width-rightPadding, thresholdY,
		strings.Join(pathParts, " "), strokeColor,
		lastX, lastY,
		8.0, topPadding+4.0, template.HTMLEscapeString(yTop),
		8.0, height-bottomPadding, template.HTMLEscapeString(yBottom),
		leftPadding, height-10.0, template.HTMLEscapeString(formatLabel(points[0].Timestamp)),
		leftPadding+plotWidth/2, height-10.0, template.HTMLEscapeString(formatLabel(midTime)),
		width-rightPadding, height-10.0, template.HTMLEscapeString(formatLabel(points[len(points)-1].Timestamp)),
		width-rightPadding, thresholdY-4.0, template.HTMLEscapeString(thresholdLabel),
	)
	return template.HTML(svg)
}

func diagnosticHTMLStatusClass(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "completed", "healthy", "created":
		return "status-ok"
	case "failed", "critical":
		return "status-critical"
	case "running", "warning":
		return "status-warn"
	case "skipped":
		return "status-skip"
	default:
		return "status-neutral"
	}
}

func diagnosticHTMLToneClass(tone string) string {
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "healthy":
		return "tone-healthy"
	case "warning":
		return "tone-warning"
	case "critical":
		return "tone-critical"
	default:
		return "tone-neutral"
	}
}

func diagnosticTaskBundleDir(taskID uint) string {
	baseDir := strings.TrimSpace(config.GetStorageConfig().BaseDir)
	if baseDir == "" {
		baseDir = "./data/storage"
	}
	return filepath.Join(baseDir, "diagnostics", "tasks", fmt.Sprintf("%d", taskID))
}

func buildDiagnosticLogCandidates(target DiagnosticTaskNodeTarget, errorEvents []*SeatunnelErrorEvent) []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, event := range errorEvents {
		if event == nil {
			continue
		}
		if target.NodeID > 0 && event.NodeID > 0 {
			if event.NodeID != target.NodeID {
				continue
			}
		} else if event.HostID != target.HostID {
			continue
		}
		if role := strings.TrimSpace(target.Role); role != "" && strings.TrimSpace(event.Role) != "" && !strings.EqualFold(event.Role, role) {
			continue
		}
		if installDir := strings.TrimSpace(target.InstallDir); installDir != "" && strings.TrimSpace(event.InstallDir) != "" && strings.TrimSpace(event.InstallDir) != installDir {
			continue
		}
		if path := strings.TrimSpace(event.SourceFile); path != "" {
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				candidates = append(candidates, path)
			}
		}
	}
	// target.InstallDir 是远端 Seatunnel 安装路径，通常运行在 Linux 上，这里必须使用 POSIX 路径拼接。
	defaultLog := path.Join(target.InstallDir, "logs", diagnosticDefaultLogFile(target.Role))
	if _, ok := seen[defaultLog]; !ok {
		candidates = append(candidates, defaultLog)
	}
	return candidates
}

var diagnosticLogTimestampPattern = regexp.MustCompile(`(?:\[[^\]]*\]\s+)?(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:,\d{3})?)`)

func (s *Service) collectDiagnosticWindowedLogSnippet(ctx context.Context, target DiagnosticTaskNodeTarget, candidate string, window diagnosticCollectionWindow, maxLines int) (string, string, error) {
	if s.agentSender == nil {
		return "", "agent sender is unavailable", fmt.Errorf("agent sender is unavailable")
	}
	contents := make([]string, 0, 4)
	days := diagnosticWindowDays(window.Start, window.End)
	for _, day := range days {
		params := map[string]string{
			"log_file": candidate,
			"mode":     "all",
		}
		if isDiagnosticCurrentLogDay(day, window.End) {
			// current active file
		} else {
			params["date"] = day.Format("2006-01-02")
		}
		success, output, err := s.sendDiagnosticAgentCommandWithRetry(ctx, target.AgentID, "get_logs", params)
		if err != nil || !success {
			continue
		}
		if strings.TrimSpace(output) != "" {
			contents = append(contents, output)
		}
	}
	if len(contents) == 0 {
		return "", bilingualText("未读取到日志文件内容。", "No log file content was read."), fmt.Errorf("no log content")
	}

	snippet, _, sawTimestamp := extractDiagnosticLogWindowContent(contents, window.Start, window.End)
	if strings.TrimSpace(snippet) == "" {
		if sawTimestamp {
			return "", bilingualText("指定时间窗内未命中日志片段。", "No log entries matched the diagnostics window."), nil
		}
		snippet = strings.TrimSpace(strings.Join(contents, "\n"))
	}
	return snippet, "", nil
}

func diagnosticWindowDays(start, end time.Time) []time.Time {
	if end.Before(start) {
		start, end = end, start
	}
	start = start.Local()
	end = end.Local()
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	days := make([]time.Time, 0, int(endDay.Sub(startDay)/(24*time.Hour))+1)
	for day := startDay; !day.After(endDay); day = day.Add(24 * time.Hour) {
		days = append(days, day)
	}
	return days
}

func isDiagnosticCurrentLogDay(day, end time.Time) bool {
	y1, m1, d1 := day.Date()
	y2, m2, d2 := end.Local().Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func extractDiagnosticLogWindowContent(contents []string, start, end time.Time) (string, bool, bool) {
	type logEntry struct {
		ts      time.Time
		hasTime bool
		lines   []string
	}
	entries := make([]logEntry, 0, 64)
	appendLine := func(current *logEntry, line string) *logEntry {
		if current == nil {
			entry := logEntry{lines: []string{line}}
			entries = append(entries, entry)
			return &entries[len(entries)-1]
		}
		current.lines = append(current.lines, line)
		return current
	}

	var current *logEntry
	for _, content := range contents {
		for _, line := range strings.Split(content, "\n") {
			if ts, ok := parseDiagnosticLogTimestamp(line); ok {
				entry := logEntry{
					ts:      ts,
					hasTime: true,
					lines:   []string{line},
				}
				entries = append(entries, entry)
				current = &entries[len(entries)-1]
				continue
			}
			current = appendLine(current, line)
		}
	}

	filtered := make([]string, 0, 256)
	sawTimestamp := false
	matchedWindow := false
	for _, entry := range entries {
		if entry.hasTime {
			sawTimestamp = true
		}
		if entry.hasTime && (entry.ts.Before(start) || entry.ts.After(end)) {
			continue
		}
		if entry.hasTime {
			matchedWindow = true
		}
		for _, line := range entry.lines {
			filtered = append(filtered, line)
		}
	}
	return strings.TrimSpace(strings.Join(filtered, "\n")), matchedWindow, sawTimestamp
}

func parseDiagnosticLogTimestamp(line string) (time.Time, bool) {
	matches := diagnosticLogTimestampPattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		return time.Time{}, false
	}
	value := strings.TrimSpace(matches[1])
	layouts := []string{"2006-01-02 15:04:05,000", "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func diagnosticDefaultLogFile(role string) string {
	switch strings.TrimSpace(role) {
	case "master":
		return "seatunnel-engine-master.log"
	case "worker":
		return "seatunnel-engine-worker.log"
	default:
		return "seatunnel-engine-server.log"
	}
}

func sortedDiagnosticTaskTargets(targets DiagnosticTaskNodeTargets) []DiagnosticTaskNodeTarget {
	items := make([]DiagnosticTaskNodeTarget, 0, len(targets))
	items = append(items, targets...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].HostID != items[j].HostID {
			return items[i].HostID < items[j].HostID
		}
		return items[i].Role < items[j].Role
	})
	return items
}

func normalizeDiagnosticFileRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" || role == "master/worker" {
		return "hybrid"
	}
	return strings.ReplaceAll(role, "/", "-")
}

const diagnosticBundleHTMLTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>SeaTunnelX Diagnostic Report</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4f7fb;
      --panel: #ffffff;
      --panel-soft: #f8fbff;
      --border: #d9e2ec;
      --border-strong: #c6d3e1;
      --muted: #64748b;
      --text: #0f172a;
      --primary: #2563eb;
      --ok: #10b981;
      --ok-soft: #ecfdf5;
      --warn: #f59e0b;
      --warn-soft: #fff7ed;
      --critical: #ef4444;
      --critical-soft: #fef2f2;
      --neutral: #3b82f6;
      --neutral-soft: #eff6ff;
      --skip: #94a3b8;
      --skip-soft: #f8fafc;
      --code-bg: #0f172a;
      --code-text: #e2e8f0;
    }
    * { box-sizing: border-box; }
    html { scroll-behavior: smooth; }
    body {
      margin: 0;
      padding: 28px;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color: var(--text);
      background: var(--bg);
      line-height: 1.6;
    }
    h1, h2, h3, h4, p { margin: 0; }
    a { color: var(--primary); text-decoration: none; }
    a:hover { text-decoration: underline; }
    .page {
      max-width: 1520px;
      margin: 0 auto;
      display: flex;
      flex-direction: column;
      gap: 18px;
    }
    .hero {
      background: var(--panel);
      border: 1px solid var(--border-strong);
      border-radius: 16px;
      padding: 24px 28px;
    }
    .hero-grid {
      display: grid;
      grid-template-columns: minmax(0, 1.35fr) minmax(320px, 0.65fr);
      gap: 24px;
      align-items: start;
    }
    .hero-badges {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-bottom: 14px;
    }
    .hero-kicker {
      color: var(--primary);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      margin-bottom: 8px;
    }
    .hero-title {
      font-size: 32px;
      line-height: 1.2;
      margin-bottom: 10px;
    }
    .hero-summary {
      color: var(--muted);
      max-width: 840px;
      font-size: 15px;
    }
    .hero-side {
      border: 1px solid var(--border);
      border-radius: 14px;
      background: var(--panel-soft);
      padding: 18px;
    }
    .hero-side.tone-healthy {
      background: linear-gradient(180deg, var(--ok-soft) 0, #fff 100%);
      border-color: rgba(16,185,129,0.28);
    }
    .hero-side.tone-warning {
      background: linear-gradient(180deg, var(--warn-soft) 0, #fff 100%);
      border-color: rgba(245,158,11,0.28);
    }
    .hero-side.tone-critical {
      background: linear-gradient(180deg, var(--critical-soft) 0, #fff 100%);
      border-color: rgba(239,68,68,0.28);
    }
    .hero-side.tone-neutral {
      background: linear-gradient(180deg, var(--neutral-soft) 0, #fff 100%);
      border-color: rgba(59,130,246,0.24);
    }
    .side-label,
    .focus-label,
    .panel-label,
    .subsection-label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      margin-bottom: 10px;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 4px 10px;
      border-radius: 999px;
      border: 1px solid var(--border);
      font-size: 12px;
      font-weight: 600;
      background: #fff;
      color: var(--text);
    }
    .status-ok { background: var(--ok-soft); border-color: rgba(16,185,129,0.32); }
    .status-warn { background: var(--warn-soft); border-color: rgba(245,158,11,0.32); }
    .status-critical { background: var(--critical-soft); border-color: rgba(239,68,68,0.32); }
    .status-neutral { background: var(--neutral-soft); border-color: rgba(59,130,246,0.24); }
    .status-skip { background: var(--skip-soft); border-color: rgba(148,163,184,0.36); }
    .metric-grid,
    .stat-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
    }
    .metric-card,
    .stat-card {
      border: 1px solid var(--border);
      border-radius: 12px;
      background: #fff;
      padding: 14px;
      min-height: 96px;
    }
    .metric-card .label,
    .stat-card .label {
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 8px;
    }
    .metric-card .value,
    .stat-card .value {
      font-size: 24px;
      font-weight: 700;
      line-height: 1.15;
    }
    .metric-card .note,
    .stat-card .note {
      color: var(--muted);
      font-size: 12px;
      margin-top: 8px;
      line-height: 1.5;
    }
    .report-nav {
      position: sticky;
      top: 12px;
      z-index: 20;
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      padding: 10px 12px;
      border: 1px solid rgba(198,211,225,0.9);
      border-radius: 14px;
      background: rgba(255,255,255,0.92);
      backdrop-filter: blur(10px);
    }
    .report-nav a {
      display: inline-flex;
      align-items: center;
      padding: 8px 12px;
      border-radius: 999px;
      background: #f8fbff;
      color: #1e293b;
      font-size: 13px;
      font-weight: 600;
    }
    .report-nav a:hover {
      background: #eef4ff;
      text-decoration: none;
    }
    .section {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
      padding: 22px 24px;
    }
    .section-heading {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 18px;
    }
    .section-lead {
      margin-top: 6px;
      color: var(--muted);
      font-size: 14px;
    }
    .grid-2 {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
      gap: 18px;
    }
    .focus-grid {
      display: grid;
      grid-template-columns: minmax(0, 1.1fr) minmax(0, 0.9fr) minmax(0, 1fr);
      gap: 16px;
    }
    .focus-panel,
    .detail-panel {
      border: 1px solid var(--border);
      border-radius: 14px;
      background: #fff;
      padding: 18px;
    }
    .focus-panel.tone-healthy {
      background: linear-gradient(180deg, var(--ok-soft) 0, #fff 100%);
      border-color: rgba(16,185,129,0.28);
    }
    .focus-panel.tone-warning {
      background: linear-gradient(180deg, var(--warn-soft) 0, #fff 100%);
      border-color: rgba(245,158,11,0.28);
    }
    .focus-panel.tone-critical {
      background: linear-gradient(180deg, var(--critical-soft) 0, #fff 100%);
      border-color: rgba(239,68,68,0.28);
    }
    .focus-panel.tone-neutral {
      background: linear-gradient(180deg, var(--neutral-soft) 0, #fff 100%);
      border-color: rgba(59,130,246,0.24);
    }
    .focus-panel h3,
    .detail-title {
      font-size: 20px;
      line-height: 1.35;
      margin-bottom: 10px;
    }
    .focus-panel p,
    .detail-panel p {
      color: var(--muted);
    }
    .panel-note {
      margin-top: 12px;
      color: var(--muted);
      font-size: 13px;
    }
    .detail-columns {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
      gap: 18px;
    }
    .dl {
      display: flex;
      flex-direction: column;
      gap: 0;
    }
    .dl-row {
      display: grid;
      grid-template-columns: 180px minmax(0, 1fr);
      gap: 14px;
      padding: 10px 0;
      border-bottom: 1px solid #edf2f7;
    }
    .dl-row:last-child { border-bottom: none; }
    .dl-term {
      color: var(--muted);
      font-size: 13px;
    }
    .dl-value {
      word-break: break-word;
      font-size: 14px;
    }
    .subsection + .subsection {
      margin-top: 18px;
      padding-top: 18px;
      border-top: 1px solid #edf2f7;
    }
    .list {
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .entry {
      padding: 14px 0;
      border-bottom: 1px solid #edf2f7;
    }
    .entry:first-child { padding-top: 0; }
    .entry:last-child {
      border-bottom: none;
      padding-bottom: 0;
    }
    .entry-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 12px;
      flex-wrap: wrap;
      margin-bottom: 8px;
    }
    .entry-title {
      font-weight: 700;
      line-height: 1.5;
    }
    .muted {
      color: var(--muted);
    }
    .muted.wrap-text,
    .wrap-text {
      white-space: normal;
      overflow-wrap: anywhere;
      word-break: break-word;
    }
    .small { font-size: 12px; }
    .callout {
      margin-top: 14px;
      padding: 14px 16px;
      border-radius: 12px;
      background: #f8fbff;
      border: 1px solid var(--border);
    }
    .callout.critical {
      background: var(--critical-soft);
      border-color: rgba(239,68,68,0.28);
    }
    .callout.warn {
      background: var(--warn-soft);
      border-color: rgba(245,158,11,0.28);
    }
    .list-clean {
      margin: 0;
      padding-left: 18px;
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .list-clean li { color: #1e293b; }
    .table-wrap {
      border: 1px solid var(--border);
      border-radius: 14px;
      overflow-x: auto;
      overflow-y: hidden;
      margin-top: 14px;
      background: #fff;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 14px;
    }
    th, td {
      padding: 12px 14px;
      text-align: left;
      border-bottom: 1px solid #edf2f7;
      vertical-align: top;
      word-break: break-word;
    }
    .process-events-table th:first-child,
    .process-events-table td:first-child {
      width: 168px;
      min-width: 168px;
      white-space: normal;
      overflow-wrap: normal;
      word-break: keep-all;
    }
    .process-events-table th:nth-child(2),
    .process-events-table td:nth-child(2) {
      width: 110px;
      min-width: 110px;
    }
    .process-events-table th:nth-child(3),
    .process-events-table td:nth-child(3) {
      width: 92px;
      min-width: 92px;
    }
    .process-events-table th:nth-child(4),
    .process-events-table td:nth-child(4) {
      width: 92px;
      min-width: 92px;
    }
    .metric-signals-table th {
      white-space: nowrap;
      word-break: normal;
      overflow-wrap: normal;
    }
    .metric-signals-table {
      width: max-content;
      min-width: 100%;
    }
    .metric-signals-table th:first-child,
    .metric-signals-table td:first-child {
      width: 132px;
      min-width: 132px;
      white-space: nowrap;
      word-break: normal;
      overflow-wrap: normal;
    }
    .metric-signals-table th:nth-child(2),
    .metric-signals-table td:nth-child(2) {
      width: 392px;
      min-width: 392px;
    }
    .metric-signals-table th:nth-child(3),
    .metric-signals-table td:nth-child(3) {
      width: 132px;
      min-width: 132px;
      white-space: nowrap;
    }
    .metric-signals-table th:nth-child(4),
    .metric-signals-table td:nth-child(4) {
      width: 84px;
      min-width: 84px;
      white-space: nowrap;
      word-break: normal;
      overflow-wrap: normal;
    }
    .metric-signals-table th:nth-child(5),
    .metric-signals-table td:nth-child(5) {
      width: 88px;
      min-width: 88px;
      white-space: nowrap;
      word-break: normal;
      overflow-wrap: normal;
    }
    th {
      background: #f8fbff;
      color: var(--muted);
      font-weight: 600;
    }
    tr:last-child td { border-bottom: none; }
    .artifact-group + .artifact-group {
      margin-top: 22px;
      padding-top: 22px;
      border-top: 1px solid #edf2f7;
    }
    .artifact-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
      gap: 14px;
    }
    .artifact-card {
      border: 1px solid var(--border);
      border-radius: 14px;
      background: #fff;
      padding: 16px;
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .artifact-meta {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .artifact-meta .meta-item {
      border: 1px solid #edf2f7;
      border-radius: 10px;
      padding: 10px 12px;
      background: #fafcff;
    }
    .meta-item .label {
      color: var(--muted);
      font-size: 12px;
      margin-bottom: 6px;
    }
    .meta-item .value {
      font-size: 13px;
      word-break: break-word;
    }
    code.inline {
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
      background: #eff6ff;
      color: #1d4ed8;
      padding: 2px 6px;
      border-radius: 6px;
      word-break: break-all;
    }
    pre {
      margin: 0;
      background: var(--code-bg);
      color: var(--code-text);
      padding: 16px;
      border-radius: 12px;
      overflow: auto;
      line-height: 1.6;
      font-size: 12px;
      max-height: 420px;
      white-space: pre-wrap;
      word-break: break-word;
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    }
    details {
      border: 1px dashed var(--border);
      border-radius: 12px;
      padding: 12px 14px;
      background: #f8fafc;
    }
    details summary {
      cursor: pointer;
      font-weight: 600;
    }
    .metric-chart {
      width: 100%;
      min-width: 320px;
      height: 156px;
      display: block;
      background: linear-gradient(180deg, #f8fbff 0%, #ffffff 100%);
      border: 1px solid #e2e8f0;
      border-radius: 10px;
    }
    .metric-chart-axis {
      stroke-width: 1.2;
    }
    .metric-chart-grid {
      stroke-width: 1;
      stroke-dasharray: 3 4;
    }
    .metric-chart-path {
      fill: none;
      stroke-width: 2.4;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .metric-chart-threshold {
      stroke: #f59e0b;
      stroke-width: 1.2;
      stroke-dasharray: 4 3;
    }
    .metric-chart-dot {
      fill: #0f172a;
    }
    .metric-chart-label {
      fill: #64748b;
      font-size: 10px;
    }
    .metric-chart-label.x-mid {
      text-anchor: middle;
    }
    .metric-chart-label.x-end,
    .metric-chart-label.threshold-label {
      text-anchor: end;
    }
    .empty {
      border: 1px dashed var(--border);
      border-radius: 12px;
      padding: 18px;
      color: var(--muted);
      background: #fafcff;
    }
    @media (max-width: 1120px) {
      .hero-grid,
      .focus-grid {
        grid-template-columns: 1fr;
      }
    }
    @media (max-width: 900px) {
      body { padding: 16px; }
      .section, .hero { padding: 18px; }
      .dl-row {
        grid-template-columns: 1fr;
        gap: 6px;
      }
      .metric-grid,
      .stat-grid,
      .artifact-meta {
        grid-template-columns: 1fr;
      }
    }
  </style>
</head>
<body>
  <div class="page">
    <header class="hero">
      <div class="hero-grid">
        <div class="hero-main">
          <div class="hero-badges">
            <span class="badge {{statusClass .Task.Status}}">{{.Task.Status}}</span>
            <span class="badge {{statusClass .Health.Tone}}">{{.Health.Title}}</span>
            <span class="badge">Task #{{.Task.ID}}</span>
            <span class="badge">Generated {{formatTime .GeneratedAt}}</span>
          </div>
          <div class="hero-kicker">SeaTunnelX</div>
          <h1 class="hero-title">诊断报告 / Diagnostic Report</h1>
          <p class="hero-summary">{{.Health.Summary}}</p>
        </div>
        <aside class="hero-side {{toneClass .Health.Tone}}">
          <div class="side-label">核心指标 / Key Signals</div>
          <div class="metric-grid">
            {{range .Health.Metrics}}
            <div class="metric-card">
              <div class="label">{{.Label}}</div>
              <div class="value">{{.Value}}</div>
              {{if .Note}}<div class="note">{{.Note}}</div>{{end}}
            </div>
            {{end}}
          </div>
        </aside>
      </div>
    </header>

    <nav class="report-nav">
      <a href="#focus">摘要</a>
      <a href="#findings">关键发现</a>
      <a href="#evidence">证据详情</a>
      <a href="#appendix">附录</a>
    </nav>

    <section class="section" id="focus">
      <div class="section-heading">
        <div>
          <h2>摘要 / Summary</h2>
          <p class="section-lead">先看本次诊断的整体结论、影响范围和建议动作，再进入关键发现与证据详情。</p>
        </div>
      </div>
      <div class="focus-grid">
        <article class="focus-panel {{toneClass .Health.Tone}}">
          <div class="focus-label">一句话结论 / Current Assessment</div>
          <h3>{{.Health.Title}}</h3>
          <p>{{.Health.Summary}}</p>
          {{if .ErrorContext}}
          <div class="panel-note">最近关联错误组：{{.ErrorContext.GroupTitle}}</div>
          {{else if .Inspection}}
          <div class="panel-note">本报告包含巡检上下文，可结合巡检发现确认问题影响范围。</div>
          {{else}}
          <div class="panel-note">当前报告主要依据任务执行证据与已收集产物生成。</div>
          {{end}}
        </article>

        <article class="focus-panel">
          <div class="focus-label">影响范围 / Impact Summary</div>
          <p>
            {{if .Inspection}}
            巡检窗口：最近 {{.Inspection.LookbackMinutes}} 分钟；共发现 {{.Inspection.CriticalCount}} 严重 / {{.Inspection.WarningCount}} 告警 / {{.Inspection.InfoCount}} 信息。
            {{else}}
            当前影响范围以错误事件、告警快照和任务采集结果为准。
            {{end}}
          </p>
        </article>

        <article class="focus-panel">
          <div class="focus-label">建议动作 / Recommended Next Step</div>
          {{if .Recommendations}}
          <ul class="list-clean">
            {{range .Recommendations}}
            <li>
              <strong>{{.Title}}</strong>
              <div class="muted small">{{.Details}}</div>
            </li>
            {{end}}
          </ul>
          {{else}}
          <div class="empty">当前报告没有生成额外建议，请直接查看错误上下文与任务执行过程。 / No extra recommendations were generated for this report.</div>
          {{end}}
        </article>
      </div>
    </section>

    <section class="section" id="findings">
      <div class="section-heading">
        <div>
          <h2>关键发现 / Critical Findings</h2>
          <p class="section-lead">按严重程度优先展示本次诊断最值得先处理的问题。</p>
        </div>
      </div>
      {{if and .Inspection .Inspection.Findings}}
      <div class="list">
        {{range .Inspection.Findings}}
        <div class="entry">
          <div class="entry-header">
            <div>
              <div class="entry-title">{{.CheckName}}</div>
              <div class="muted small">{{.CheckCode}}</div>
            </div>
            <span class="badge {{statusClass .Severity}}">{{.Severity}}</span>
          </div>
          {{if .Summary}}<div>{{.Summary}}</div>{{end}}
          {{if .Evidence}}<div class="muted small" style="margin-top: 6px;">{{.Evidence}}</div>{{end}}
          {{if .Recommendation}}<div class="muted small" style="margin-top: 6px;">{{.Recommendation}}</div>{{end}}
        </div>
        {{end}}
      </div>
      {{else if .ErrorContext}}
      <div class="focus-grid">
        <article class="focus-panel critical">
          <div class="focus-label">错误组 / Error Group</div>
          <h3>{{.ErrorContext.GroupTitle}}</h3>
          <p>{{.ErrorContext.SampleMessage}}</p>
          <div class="panel-note">最近窗口内事件数：{{.ErrorContext.RecentEventCount}}</div>
        </article>
      </div>
      {{else}}
      <div class="empty">当前诊断窗口内没有生成结构化关键发现，请继续查看证据详情确认是否存在偶发问题。 / No structured critical findings were generated in this window.</div>
      {{end}}
    </section>

    <section class="section" id="evidence">
      <div class="section-heading">
        <div>
          <h2>证据详情 / Evidence Details</h2>
          <p class="section-lead">这一部分用于回答“问题是什么、是否仍在发生、影响到了哪里”。</p>
        </div>
      </div>

      <div class="grid-2">
        <div class="detail-panel">
          <div class="panel-label">错误上下文 / Error Context</div>
          {{if .ErrorContext}}
          <div class="dl">
            <div class="dl-row"><div class="dl-term">错误组</div><div class="dl-value">{{.ErrorContext.GroupTitle}}</div></div>
            <div class="dl-row"><div class="dl-term">异常类型</div><div class="dl-value">{{.ErrorContext.ExceptionClass}}</div></div>
            <div class="dl-row"><div class="dl-term">累计次数</div><div class="dl-value">{{.ErrorContext.OccurrenceCount}}</div></div>
            <div class="dl-row"><div class="dl-term">最近事件数</div><div class="dl-value">{{.ErrorContext.RecentEventCount}}</div></div>
            <div class="dl-row"><div class="dl-term">首次出现</div><div class="dl-value">{{formatTime .ErrorContext.FirstSeenAt}}</div></div>
            <div class="dl-row"><div class="dl-term">最近出现</div><div class="dl-value">{{formatTime .ErrorContext.LastSeenAt}}</div></div>
          </div>
          <div class="callout critical">{{.ErrorContext.SampleMessage}}</div>
          {{if .ErrorContext.Events}}
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Occurred At</th>
                  <th>Host</th>
                  <th>Role</th>
                  <th>Job ID</th>
                  <th>Source File</th>
                </tr>
              </thead>
              <tbody>
                {{range .ErrorContext.Events}}
                <tr>
                  <td>{{.OccurredAt}}</td>
                  <td>{{.HostLabel}}</td>
                  <td>{{.Role}}</td>
                  <td>{{.JobID}}</td>
                  <td><code class="inline">{{.SourceFile}}</code></td>
                </tr>
                <tr>
                  <td colspan="5">
                    <div>{{.Message}}</div>
                    {{if .Evidence}}<div class="muted small" style="margin-top: 6px;">{{.Evidence}}</div>{{end}}
                  </td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
          {{end}}
          {{if .ErrorContext.LogSamples}}
          <div class="subsection">
            <div class="subsection-label">原始错误日志预览 / Raw Error Log Preview</div>
            <div class="list">
              {{range .ErrorContext.LogSamples}}
              <div class="entry">
                <div class="entry-header">
                  <div>
                    <div class="entry-title">{{.HostLabel}}</div>
                    <div class="muted small"><code class="inline">{{.SourceFile}}</code></div>
                  </div>
                  <span class="badge">{{.WindowLabel}}</span>
                </div>
                <pre>{{.Content}}</pre>
              </div>
              {{end}}
            </div>
          </div>
          {{end}}
          {{else}}
          <div class="empty">当前诊断报告未附带错误组上下文。 / No error-group context is attached to this report.</div>
          {{end}}
        </div>

        <div class="detail-panel">
          <div class="panel-label">巡检上下文 / Inspection Context</div>
          {{if .Inspection}}
          <div class="dl">
            <div class="dl-row"><div class="dl-term">Summary</div><div class="dl-value">{{.Inspection.Summary}}</div></div>
            <div class="dl-row"><div class="dl-term">Status</div><div class="dl-value"><span class="badge {{statusClass .Inspection.Status}}">{{.Inspection.Status}}</span></div></div>
            <div class="dl-row"><div class="dl-term">Requested By</div><div class="dl-value">{{.Inspection.RequestedBy}}</div></div>
            <div class="dl-row"><div class="dl-term">Lookback Window</div><div class="dl-value">{{.Inspection.LookbackMinutes}} min</div></div>
            <div class="dl-row"><div class="dl-term">Started At</div><div class="dl-value">{{formatTime .Inspection.StartedAt}}</div></div>
            <div class="dl-row"><div class="dl-term">Finished At</div><div class="dl-value">{{formatTime .Inspection.FinishedAt}}</div></div>
          </div>
          {{else}}
          <div class="empty">当前诊断报告没有巡检详情上下文。 / No inspection context is attached to this report.</div>
          {{end}}
        </div>
      </div>

      <div class="grid-2" style="margin-top: 18px;">
        <div class="detail-panel">
          <div class="panel-label">运行配置文件 / Runtime Config Files</div>
          {{if .ConfigSnapshot}}
          <div class="stat-grid">
            <div class="stat-card"><div class="label">Files</div><div class="value">{{.ConfigSnapshot.FileCount}}</div></div>
            <div class="stat-card"><div class="label">Key Settings</div><div class="value">{{.ConfigSnapshot.KeyHighlightCount}}</div></div>
            <div class="stat-card"><div class="label">Inventories</div><div class="value">{{.ConfigSnapshot.DirectoryCount}}</div></div>
            <div class="stat-card"><div class="label">DB Changes</div><div class="value">{{.ConfigSnapshot.ChangedConfigCount}}</div></div>
          </div>
          {{if .ConfigSnapshot.KeyHighlights}}
          <div class="subsection">
            <div class="subsection-label">关键配置摘要 / Key Runtime Settings</div>
            <div class="list">
              {{range .ConfigSnapshot.KeyHighlights}}
              <div class="entry">
                <div class="entry-header">
                  <div>
                    <div class="entry-title">{{.ConfigType}}</div>
                    <div class="muted small">{{if .HostName}}{{.HostName}}{{else}}Host #{{.HostID}}{{end}} / {{.Role}}</div>
                  </div>
                </div>
                <div class="muted small"><code class="inline">{{.RemotePath}}</code></div>
                <div class="dl">
                  {{range .Items}}
                  <div class="dl-row">
                    <div class="dl-term">{{.Label}}</div>
                    <div class="dl-value">{{.Value}}</div>
                  </div>
                  {{end}}
                </div>
              </div>
              {{end}}
            </div>
          </div>
          {{end}}
          {{if .ConfigSnapshot.FilePreviews}}
          <div class="subsection">
            <div class="subsection-label">配置文件预览 / Runtime Config Preview</div>
            <div class="list">
              {{range .ConfigSnapshot.FilePreviews}}
              <div class="entry">
                <div class="entry-header">
                  <div>
                    <div class="entry-title">{{.ConfigType}}</div>
                    <div class="muted small">{{if .HostName}}{{.HostName}}{{else}}Host #{{.HostID}}{{end}} / {{.Role}}</div>
                  </div>
                </div>
                <div class="muted small"><code class="inline">{{.RemotePath}}</code></div>
                <pre>{{.Preview}}</pre>
              </div>
              {{end}}
            </div>
          </div>
          {{end}}
          {{if .ConfigSnapshot.RecentChanges}}
          <div class="subsection">
            <div class="subsection-label">窗口内变化轨迹 / Change Timeline</div>
            <div class="list">
              {{range .ConfigSnapshot.RecentChanges}}
              <div class="entry">
                <div class="entry-header">
                  <div>
                    <div class="entry-title">{{.ConfigType}}</div>
                    <div class="muted small">{{.HostScope}} · version {{.Version}}</div>
                  </div>
                  <span class="badge">{{formatTime .UpdatedAt}}</span>
                </div>
                <div><code class="inline">{{.FilePath}}</code></div>
              </div>
              {{end}}
            </div>
            {{if .ConfigSnapshot.RemainingChanges}}
            <details style="margin-top: 12px;">
              <summary>查看其余配置变更 / View remaining config changes ({{len .ConfigSnapshot.RemainingChanges}})</summary>
              <div class="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Updated At</th>
                      <th>Config Type</th>
                      <th>Scope</th>
                      <th>Version</th>
                      <th>Path</th>
                    </tr>
                  </thead>
                  <tbody>
                    {{range .ConfigSnapshot.RemainingChanges}}
                    <tr>
                      <td>{{formatTime .UpdatedAt}}</td>
                      <td>{{.ConfigType}}</td>
                      <td>{{.HostScope}}</td>
                      <td>{{.Version}}</td>
                      <td><code class="inline">{{.FilePath}}</code></td>
                    </tr>
                    {{end}}
                  </tbody>
                </table>
              </div>
            </details>
            {{end}}
          </div>
          {{end}}
          {{if .ConfigSnapshot.Files}}
          <div class="subsection">
            <details>
              <summary>查看原始运行配置文件清单 / View raw runtime config files</summary>
              <div class="table-wrap">
                <table class="metric-signals-table">
                  <thead>
                    <tr>
                      <th>Host</th>
                      <th>Role</th>
                      <th>Type</th>
                      <th>Remote Path</th>
                      <th>Size</th>
                      <th>Hash</th>
                    </tr>
                  </thead>
                  <tbody>
                    {{range .ConfigSnapshot.Files}}
                    <tr>
                      <td>{{if .HostName}}{{.HostName}}{{else}}Host #{{.HostID}}{{end}}</td>
                      <td>{{.Role}}</td>
                      <td>{{.ConfigType}}</td>
                      <td><code class="inline">{{.RemotePath}}</code></td>
                      <td>{{formatBytes .SizeBytes}}</td>
                      <td><code class="inline">{{shortHash .ContentHash}}</code></td>
                    </tr>
                    {{end}}
                  </tbody>
                </table>
              </div>
            </details>
          </div>
          {{end}}
          {{if .ConfigSnapshot.DirectoryManifests}}
          <div class="subsection">
            <details>
              <summary>查看目录清单（配置 / 依赖 / 连接器） / View directory inventories</summary>
              <div class="list" style="margin-top: 12px;">
                {{range .ConfigSnapshot.DirectoryManifests}}
                <div class="entry">
                  <div class="entry-header">
                    <div class="entry-title">{{.Directory}}</div>
                    <span class="badge">{{.EntryCount}} entries</span>
                  </div>
                  <div class="muted small">Host #{{.HostID}} / {{.Role}}</div>
                  <div class="table-wrap" style="margin-top: 10px;">
                    <table>
                      <thead>
                        <tr>
                          <th>Name</th>
                          <th>Path</th>
                          <th>Size</th>
                          <th>Modified</th>
                        </tr>
                      </thead>
                      <tbody>
                        {{range .Entries}}
                        <tr>
                          <td>{{.Name}}</td>
                          <td><code class="inline">{{.Path}}</code></td>
                          <td>{{formatBytes .Size}}</td>
                          <td>{{formatTime .ModTime}}</td>
                        </tr>
                        {{end}}
                      </tbody>
                    </table>
                  </div>
                </div>
                {{end}}
              </div>
            </details>
          </div>
          {{end}}
          {{if .ConfigSnapshot.CollectionNotes}}
          <div class="subsection">
            <details>
              <summary>查看采集备注 / View collection notes</summary>
              <div class="list" style="margin-top: 12px;">
                {{range .ConfigSnapshot.CollectionNotes}}
                <div class="entry">
                  <div class="entry-header">
                    <div class="entry-title">{{if .ConfigType}}{{.ConfigType}}{{else}}note{{end}}</div>
                    <span class="badge">{{if .Role}}{{.Role}}{{else}}system{{end}}</span>
                  </div>
                  <div class="muted small">
                    {{if .HostID}}Host #{{.HostID}}{{else}}Cluster scope{{end}}
                  </div>
                  <div style="margin-top: 6px;">{{.Message}}</div>
                </div>
                {{end}}
              </div>
            </details>
          </div>
          {{end}}
          {{else}}
          <div class="empty">未采集到 SeaTunnel 运行配置文件。 / No runtime config files were collected.</div>
          {{end}}
        </div>

        <div class="detail-panel">
          <div class="panel-label">Prometheus 指标快照 / Prometheus Metric Snapshot</div>
          {{if .MetricsSnapshot}}
          <div class="stat-grid">
            <div class="stat-card"><div class="label">Signals</div><div class="value">{{.MetricsSnapshot.SignalCount}}</div></div>
            <div class="stat-card"><div class="label">Anomalies</div><div class="value">{{.MetricsSnapshot.AnomalyCount}}</div></div>
          </div>
          {{if .MetricsSnapshot.CollectionNotes}}
          <div class="subsection">
            <div class="subsection-label">采集备注 / Collection Notes</div>
            <div class="list">
              {{range .MetricsSnapshot.CollectionNotes}}
              <div class="entry">
                <div>{{.}}</div>
              </div>
              {{end}}
            </div>
          </div>
          {{end}}
          <div class="subsection">
            <div class="subsection-label">优先关注指标 / Prioritized Signals</div>
            <div class="list">
            {{range .MetricsSnapshot.HighlightedSignals}}
            <div class="entry">
              <div class="entry-header">
                <div>
                  <div class="entry-title">{{.Title}}</div>
                  <div class="muted small">{{.ThresholdText}}</div>
                </div>
                <span class="badge {{statusClass .Status}}">{{.Status}}</span>
              </div>
              <div>{{.Summary}}</div>
              {{if .Series}}
              {{$unit := .Unit}}
              {{$threshold := .Threshold}}
              {{$comparator := .Comparator}}
              <div class="table-wrap" style="margin-top: 10px;">
                <table>
                  <thead>
                    <tr>
                      <th>Instance</th>
                      <th>Curve</th>
                      <th>Peak</th>
                      <th>Last</th>
                      <th>Samples</th>
                    </tr>
                  </thead>
                  <tbody>
                    {{range .Series}}
                    <tr>
                      <td>{{.Instance}}</td>
                      <td>{{metricChartSVG .Points $threshold $comparator $unit}}</td>
                      <td>{{formatMetricValue $unit .MaxValue}}<div class="muted small">{{formatTime .MaxAt}}</div></td>
                      <td>{{formatMetricValue $unit .LastValue}}</td>
                      <td>{{.Samples}}</td>
                    </tr>
                    {{end}}
                  </tbody>
                </table>
              </div>
              {{end}}
            </div>
            {{end}}
            </div>
          </div>
          {{if .MetricsSnapshot.AdditionalSignals}}
          <div class="subsection">
            <details>
              <summary>查看其余指标信号 / View remaining signals ({{len .MetricsSnapshot.AdditionalSignals}})</summary>
              <div class="list" style="margin-top: 12px;">
                {{range .MetricsSnapshot.AdditionalSignals}}
                <div class="entry">
                  <div class="entry-header">
                    <div>
                      <div class="entry-title">{{.Title}}</div>
                      <div class="muted small">{{.ThresholdText}}</div>
                    </div>
                    <span class="badge {{statusClass .Status}}">{{.Status}}</span>
                  </div>
                  <div>{{.Summary}}</div>
                </div>
                {{end}}
              </div>
            </details>
          </div>
          {{end}}
          {{else}}
          <div class="empty">未采集到 Prometheus 指标快照。 / No Prometheus metrics snapshot was collected.</div>
          {{end}}
        </div>
      </div>
    </section>

    <section class="section" id="operations">
      <div class="section-heading">
        <div>
          <h2>附录：告警与进程 / Appendix: Alerts & Process Signals</h2>
          <p class="section-lead">这部分主要用于补充当时的告警态势和进程事件，优先级低于错误、配置和指标本身。</p>
        </div>
      </div>
      <div class="detail-columns">
        <div class="detail-panel">
          <div class="panel-label">告警快照 / Alert Snapshot</div>
          {{if .AlertSnapshot}}
          <div class="stat-grid">
            <div class="stat-card"><div class="label">Total Alerts</div><div class="value">{{.AlertSnapshot.Total}}</div></div>
            <div class="stat-card"><div class="label">Critical</div><div class="value">{{.AlertSnapshot.Critical}}</div></div>
            <div class="stat-card"><div class="label">Warning</div><div class="value">{{.AlertSnapshot.Warning}}</div></div>
            <div class="stat-card"><div class="label">Firing</div><div class="value">{{.AlertSnapshot.Firing}}</div></div>
          </div>
          <div class="dl" style="margin-top: 14px;">
            <div class="dl-row"><div class="dl-term">首次告警 / First Seen</div><div class="dl-value">{{.AlertSnapshot.FirstSeenAt}}</div></div>
            <div class="dl-row"><div class="dl-term">最近告警 / Last Seen</div><div class="dl-value">{{.AlertSnapshot.LastSeenAt}}</div></div>
          </div>
          <div class="subsection">
            <div class="subsection-label">告警明细 / Alert Details</div>
            <div class="list">
              {{range .AlertSnapshot.Alerts}}
              <div class="entry">
                <div class="entry-header">
                  <div>
                    <div class="entry-title">{{.Name}}</div>
                    <div class="muted small wrap-text">Created {{.CreatedAt}} · Firing {{.FiringAt}} · Last Seen {{.LastSeenAt}}</div>
                  </div>
                  <div style="display:flex; gap:8px; flex-wrap:wrap;">
                    <span class="badge {{statusClass .Severity}}">{{.Severity}}</span>
                    <span class="badge {{statusClass .Status}}">{{.Status}}</span>
                  </div>
                </div>
                {{if ne .ResolvedAt "-"}}<div class="muted small wrap-text">Resolved {{.ResolvedAt}}</div>{{end}}
                {{if .Summary}}<div style="margin-top: 8px;">{{.Summary}}</div>{{end}}
                {{if .Description}}<div class="muted wrap-text" style="margin-top: 8px;">{{.Description}}</div>{{end}}
              </div>
              {{end}}
            </div>
          </div>
          {{else}}
          <div class="empty">未采集到活动告警。 / No alert snapshot was collected.</div>
          {{end}}
        </div>

        <div class="detail-panel">
          <div class="panel-label">进程信号 / Process Signals</div>
          {{if .ProcessEvents}}
          <div class="stat-grid">
            <div class="stat-card"><div class="label">Total Events</div><div class="value">{{.ProcessEvents.Total}}</div></div>
            {{range .ProcessEvents.ByType}}
            <div class="stat-card"><div class="label">{{.Label}}</div><div class="value">{{.Value}}</div></div>
            {{end}}
          </div>
          <div class="table-wrap" style="margin-top: 14px;">
            <table class="process-events-table">
              <thead>
                <tr>
                  <th>Created At</th>
                  <th>Event Type</th>
                  <th>Process</th>
                  <th>Node</th>
                  <th>Details</th>
                </tr>
              </thead>
              <tbody>
                {{range .ProcessEvents.Events}}
                <tr>
                  <td>{{.CreatedAt}}</td>
                  <td>{{.EventType}}</td>
                  <td>{{.ProcessName}}</td>
                  <td>{{.NodeLabel}}</td>
                  <td>{{.Details}}</td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
          {{else}}
          <div class="empty">未采集到近期进程事件。 / No process events were collected.</div>
          {{end}}
        </div>
      </div>
    </section>

    <section class="section" id="overview">
      <div class="section-heading">
        <div>
          <h2>附录：任务概览 / Appendix: Task Overview</h2>
          <p class="section-lead">这部分主要用于补充报告来源、采集选项和目标节点，排查时按需查看即可。</p>
        </div>
      </div>

      <div class="detail-columns">
        <div class="detail-panel">
          <div class="panel-label">任务信息 / Task Metadata</div>
          <div class="dl">
            <div class="dl-row"><div class="dl-term">任务摘要 / Summary</div><div class="dl-value">{{.Task.Summary}}</div></div>
            <div class="dl-row"><div class="dl-term">创建人 / Created By</div><div class="dl-value">{{.Task.CreatedBy}}</div></div>
            <div class="dl-row"><div class="dl-term">开始时间 / Started At</div><div class="dl-value">{{formatTime .Task.StartedAt}}</div></div>
            <div class="dl-row"><div class="dl-term">完成时间 / Completed At</div><div class="dl-value">{{formatTime .Task.CompletedAt}}</div></div>
            <div class="dl-row"><div class="dl-term">诊断包目录 / Bundle Dir</div><div class="dl-value"><code class="inline">{{.Task.BundleDir}}</code></div></div>
            <div class="dl-row"><div class="dl-term">Manifest</div><div class="dl-value"><code class="inline">{{.Task.ManifestPath}}</code></div></div>
            <div class="dl-row"><div class="dl-term">Report Index</div><div class="dl-value"><code class="inline">{{.Task.IndexPath}}</code></div></div>
          </div>
        </div>

        <div class="detail-panel">
          <div class="panel-label">来源与选项 / Source & Options</div>
          <div class="dl">
            {{range .SourceTraceability}}
            <div class="dl-row"><div class="dl-term">{{.Label}}</div><div class="dl-value">{{.Value}}</div></div>
            {{end}}
            <div class="dl-row"><div class="dl-term">Thread Dump</div><div class="dl-value">{{if .Task.Options.IncludeThreadDump}}Enabled{{else}}Disabled{{end}}</div></div>
            <div class="dl-row"><div class="dl-term">JVM Dump</div><div class="dl-value">{{if .Task.Options.IncludeJVMDump}}Enabled{{else}}Disabled{{end}}</div></div>
            <div class="dl-row"><div class="dl-term">Min Free Space for JVM Dump</div><div class="dl-value">{{.Task.Options.JVMDumpMinFreeMB}} MB</div></div>
          </div>
        </div>
      </div>

      <div class="detail-columns" style="margin-top: 18px;">
        <div class="detail-panel">
          <div class="panel-label">目标节点 / Selected Nodes</div>
          {{if .Task.SelectedNodes}}
          <div class="table-wrap" style="margin-top: 0;">
            <table>
              <thead>
                <tr>
                  <th>Host</th>
                  <th>Role</th>
                  <th>Cluster Node</th>
                  <th>Install Dir</th>
                </tr>
              </thead>
              <tbody>
                {{range .Task.SelectedNodes}}
                <tr>
                  <td>{{.HostLabel}}</td>
                  <td>{{.Role}}</td>
                  <td>{{.ClusterNode}}</td>
                  <td><code class="inline">{{.InstallDir}}</code></td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
          {{else}}
          <div class="empty">No selected nodes recorded.</div>
          {{end}}
        </div>

        <div class="detail-panel">
          <div class="panel-label">已确认正常 / Confirmed Normal</div>
          {{if .PassedChecks}}
          <div class="list">
            {{range .PassedChecks}}
            <div class="entry">
              <div class="entry-title">{{.Title}}</div>
              <div class="muted" style="margin-top: 6px;">{{.Details}}</div>
            </div>
            {{end}}
          </div>
          {{else}}
          <div class="empty">当前没有可展示的已通过项。 / No passed checks are available for this report.</div>
          {{end}}
        </div>
      </div>
    </section>

    <section class="section" id="execution">
      <div class="section-heading">
        <div>
          <h2>附录：执行过程 / Appendix: Task Execution</h2>
          <p class="section-lead">用于确认采集步骤是否完整执行、哪些步骤或节点失败；不是报告主体的第一阅读入口。</p>
        </div>
      </div>
      <div class="grid-2">
        <div class="detail-panel">
          <div class="panel-label">步骤状态 / Steps</div>
          {{if .TaskExecution.Steps}}
          <div class="table-wrap" style="margin-top: 0;">
            <table>
              <thead>
                <tr>
                  <th>#</th>
                  <th>Step</th>
                  <th>Status</th>
                  <th>Message</th>
                  <th>Time</th>
                </tr>
              </thead>
              <tbody>
                {{range .TaskExecution.Steps}}
                <tr>
                  <td>{{.Sequence}}</td>
                  <td><strong>{{.Title}}</strong><div class="muted small">{{.Code}}</div></td>
                  <td><span class="badge {{statusClass .Status}}">{{.Status}}</span></td>
                  <td>{{if ne .Error "-"}}{{.Error}}{{else}}{{.Message}}{{end}}</td>
                  <td>{{.StartedAt}} → {{.CompletedAt}}</td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
          {{else}}
          <div class="empty">No task steps recorded.</div>
          {{end}}
        </div>

        <div class="detail-panel">
          <div class="panel-label">节点执行 / Node Execution</div>
          {{if .TaskExecution.Nodes}}
          <div class="table-wrap" style="margin-top: 0;">
            <table>
              <thead>
                <tr>
                  <th>Host</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>Current Step</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {{range .TaskExecution.Nodes}}
                <tr>
                  <td>{{.HostLabel}}</td>
                  <td>{{.Role}}</td>
                  <td><span class="badge {{statusClass .Status}}">{{.Status}}</span></td>
                  <td>{{.CurrentStep}}</td>
                  <td>{{if ne .Error "-"}}{{.Error}}{{else}}{{.Message}}{{end}}</td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
          {{else}}
          <div class="empty">No node executions recorded.</div>
          {{end}}
        </div>
      </div>
    </section>

    <section class="section" id="appendix">
      <div class="section-heading">
        <div>
          <h2>附录：集群快照 / Appendix: Cluster Snapshot</h2>
          <p class="section-lead">用于补充部署元信息；通常在确认问题后再查阅即可。</p>
        </div>
      </div>
      {{if .Cluster}}
      <div class="detail-columns">
        <div class="detail-panel">
          <div class="panel-label">集群快照 / Cluster Snapshot</div>
          <div class="dl">
            <div class="dl-row"><div class="dl-term">Name</div><div class="dl-value">{{.Cluster.Name}}</div></div>
            <div class="dl-row"><div class="dl-term">Version</div><div class="dl-value">{{.Cluster.Version}}</div></div>
            <div class="dl-row"><div class="dl-term">Status</div><div class="dl-value"><span class="badge {{statusClass .Cluster.Status}}">{{.Cluster.Status}}</span></div></div>
            <div class="dl-row"><div class="dl-term">Deployment</div><div class="dl-value">{{.Cluster.DeploymentMode}}</div></div>
            <div class="dl-row"><div class="dl-term">Install Dir</div><div class="dl-value"><code class="inline">{{.Cluster.InstallDir}}</code></div></div>
            <div class="dl-row"><div class="dl-term">Node Count</div><div class="dl-value">{{.Cluster.NodeCount}}</div></div>
          </div>
        </div>
        <div class="detail-panel">
          <div class="panel-label">节点快照 / Nodes</div>
          <div class="table-wrap" style="margin-top: 0;">
            <table>
              <thead>
                <tr>
                  <th>Host ID</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>PID</th>
                  <th>Install Dir</th>
                </tr>
              </thead>
              <tbody>
                {{range .Cluster.Nodes}}
                <tr>
                  <td>#{{.HostID}}</td>
                  <td>{{.Role}}</td>
                  <td><span class="badge {{statusClass .Status}}">{{.Status}}</span></td>
                  <td>{{.ProcessPID}}</td>
                  <td><code class="inline">{{.InstallDir}}</code></td>
                </tr>
                {{end}}
              </tbody>
            </table>
          </div>
        </div>
      </div>
      {{else}}
      <div class="empty">未采集到集群快照。 / Cluster snapshot was not collected.</div>
      {{end}}
    </section>
  </div>
</body>
</html>`

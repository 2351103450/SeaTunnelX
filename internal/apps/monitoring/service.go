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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seatunnel/seatunnelX/internal/apps/cluster"
	"github.com/seatunnel/seatunnelX/internal/apps/monitor"
	"github.com/seatunnel/seatunnelX/internal/config"
	"github.com/seatunnel/seatunnelX/internal/logger"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// Service provides monitoring center data for UI.
// Service 为 UI 提供监控中心数据。
type Service struct {
	clusterService *cluster.Service
	monitorService *monitor.Service
	repo           *Repository
	syncMu         sync.Mutex
}

// NewService creates a monitoring service.
// NewService 创建监控中心服务。
func NewService(clusterService *cluster.Service, monitorService *monitor.Service, repo *Repository) *Service {
	return &Service{
		clusterService: clusterService,
		monitorService: monitorService,
		repo:           repo,
	}
}

// GetOverview returns global monitoring overview data.
// GetOverview 返回全局监控总览数据。
func (s *Service) GetOverview(ctx context.Context) (*OverviewData, error) {
	now := time.Now().UTC()
	since24h := now.Add(-24 * time.Hour)
	since1h := now.Add(-1 * time.Hour)

	clusters, _, err := s.clusterService.List(ctx, &cluster.ClusterFilter{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	result := &OverviewData{
		GeneratedAt: now,
		Stats: &OverviewStats{
			TotalClusters: len(clusters),
		},
		EventStats24h: &EventStats{},
		Clusters:      make([]*ClusterMonitoringSummary, 0, len(clusters)),
	}

	for _, c := range clusters {
		status, err := s.clusterService.GetStatus(ctx, c.ID)
		if err != nil {
			continue
		}

		eventStats24h, err := s.monitorService.GetEventStats(ctx, c.ID, &since24h)
		if err != nil {
			eventStats24h = map[monitor.ProcessEventType]int64{}
		}
		eventStats1h, err := s.monitorService.GetEventStats(ctx, c.ID, &since1h)
		if err != nil {
			eventStats1h = map[monitor.ProcessEventType]int64{}
		}

		e24 := toEventStats(eventStats24h)
		e1 := toEventStats(eventStats1h)

		var lastEventAt *time.Time
		recentEvents, err := s.monitorService.ListClusterEvents(ctx, c.ID, 1)
		if err == nil && len(recentEvents) > 0 {
			t := recentEvents[0].CreatedAt
			lastEventAt = &t
		}

		summary := &ClusterMonitoringSummary{
			ClusterID:              c.ID,
			ClusterName:            c.Name,
			Status:                 string(status.Status),
			HealthStatus:           string(status.HealthStatus),
			TotalNodes:             status.TotalNodes,
			OnlineNodes:            status.OnlineNodes,
			OfflineNodes:           status.OfflineNodes,
			CrashedEvents24h:       e24.Crashed,
			RestartFailedEvents24h: e24.RestartFailed + e24.RestartLimitReached,
			ActiveAlerts1h:         e1.CriticalCount(),
			LastEventAt:            lastEventAt,
		}
		result.Clusters = append(result.Clusters, summary)

		// Aggregate global stats / 聚合全局统计
		result.Stats.TotalNodes += status.TotalNodes
		result.Stats.OnlineNodes += status.OnlineNodes
		result.Stats.OfflineNodes += status.OfflineNodes

		switch status.HealthStatus {
		case cluster.HealthStatusHealthy:
			result.Stats.HealthyClusters++
		case cluster.HealthStatusUnhealthy:
			result.Stats.UnhealthyClusters++
		default:
			result.Stats.UnknownClusters++
		}

		result.Stats.CrashedEvents24h += e24.Crashed
		result.Stats.RestartFailed24h += e24.RestartFailed + e24.RestartLimitReached
		result.Stats.ActiveAlerts1h += e1.CriticalCount()

		mergeEventStats(result.EventStats24h, e24)
	}

	// Sort: unhealthy first, then active alerts, then latest events
	// 排序：异常集群优先，其次活跃告警更多，再按最近事件倒序
	sort.Slice(result.Clusters, func(i, j int) bool {
		left := result.Clusters[i]
		right := result.Clusters[j]

		leftUnhealthy := left.HealthStatus == string(cluster.HealthStatusUnhealthy)
		rightUnhealthy := right.HealthStatus == string(cluster.HealthStatusUnhealthy)
		if leftUnhealthy != rightUnhealthy {
			return leftUnhealthy
		}
		if left.ActiveAlerts1h != right.ActiveAlerts1h {
			return left.ActiveAlerts1h > right.ActiveAlerts1h
		}
		if left.LastEventAt == nil {
			return false
		}
		if right.LastEventAt == nil {
			return true
		}
		return left.LastEventAt.After(*right.LastEventAt)
	})

	return result, nil
}

// GetClusterOverview returns monitoring detail data for one cluster.
// GetClusterOverview 返回单集群监控详情数据。
func (s *Service) GetClusterOverview(ctx context.Context, clusterID uint) (*ClusterOverviewData, error) {
	now := time.Now().UTC()
	since24h := now.Add(-24 * time.Hour)
	since1h := now.Add(-1 * time.Hour)

	status, err := s.clusterService.GetStatus(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	config, err := s.monitorService.GetOrCreateConfig(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	eventStats24hRaw, err := s.monitorService.GetEventStats(ctx, clusterID, &since24h)
	if err != nil {
		return nil, err
	}
	eventStats1hRaw, err := s.monitorService.GetEventStats(ctx, clusterID, &since1h)
	if err != nil {
		return nil, err
	}

	e24 := toEventStats(eventStats24hRaw)
	e1 := toEventStats(eventStats1hRaw)

	events, _, err := s.monitorService.ListEvents(ctx, &monitor.ProcessEventFilter{
		ClusterID: clusterID,
		Page:      1,
		PageSize:  50,
	})
	if err != nil {
		return nil, err
	}

	nodes := make([]*NodeSnapshot, 0, len(status.Nodes))
	for _, node := range status.Nodes {
		nodes = append(nodes, &NodeSnapshot{
			NodeID:     node.NodeID,
			HostID:     node.HostID,
			HostName:   node.HostName,
			HostIP:     node.HostIP,
			Role:       string(node.Role),
			Status:     string(node.Status),
			IsOnline:   node.IsOnline,
			ProcessPID: node.ProcessPID,
		})
	}

	return &ClusterOverviewData{
		GeneratedAt: now,
		Cluster: &ClusterBaseInfo{
			ClusterID:    status.ClusterID,
			ClusterName:  status.ClusterName,
			Status:       string(status.Status),
			HealthStatus: string(status.HealthStatus),
		},
		Stats: &ClusterDetailStats{
			TotalNodes:             status.TotalNodes,
			OnlineNodes:            status.OnlineNodes,
			OfflineNodes:           status.OfflineNodes,
			CrashedEvents24h:       e24.Crashed,
			RestartFailedEvents24h: e24.RestartFailed + e24.RestartLimitReached,
			ActiveAlerts1h:         e1.CriticalCount(),
		},
		EventStats24h: e24,
		EventStats1h:  e1,
		MonitorConfig: config,
		Nodes:         nodes,
		RecentEvents:  events,
	}, nil
}

// ListAlerts returns alert center data.
// ListAlerts 返回告警中心数据。
func (s *Service) ListAlerts(ctx context.Context, filter *AlertFilter) (*AlertListData, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if filter == nil {
		filter = &AlertFilter{}
	}
	if filter.Status != "" && filter.Status != AlertStatusFiring && filter.Status != AlertStatusAcknowledged && filter.Status != AlertStatusSilenced {
		return nil, fmt.Errorf("invalid alert status")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	rows, total, err := s.repo.ListCriticalEvents(ctx, &AlertEventQueryFilter{
		ClusterID: filter.ClusterID,
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
		Page:      filter.Page,
		PageSize:  filter.PageSize,
	})
	if err != nil {
		return nil, err
	}

	eventIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		eventIDs = append(eventIDs, row.ID)
	}
	stateMap, err := s.repo.ListEventStatesByEventIDs(ctx, eventIDs)
	if err != nil {
		return nil, err
	}

	result := &AlertListData{
		GeneratedAt: time.Now().UTC(),
		Page:        filter.Page,
		PageSize:    filter.PageSize,
		Total:       total,
		Stats:       &AlertStats{},
		Alerts:      make([]*AlertEvent, 0, len(rows)),
	}

	ruleCache := make(map[uint]map[string]*AlertRule)
	for _, row := range rows {
		rules, err := s.getClusterRuleMap(ctx, row.ClusterID, ruleCache)
		if err != nil {
			return nil, err
		}

		ruleKey := eventTypeToRuleKey(row.EventType)
		rule := rules[ruleKey]
		if rule == nil {
			rule = defaultRuleByKey(row.ClusterID, ruleKey)
		}
		if rule == nil || !rule.Enabled {
			continue
		}

		state := stateMap[row.ID]
		status := resolveAlertStatus(state, result.GeneratedAt)
		if filter.Status != "" && status != filter.Status {
			continue
		}

		alert := &AlertEvent{
			AlertID:          fmt.Sprintf("event-%d", row.ID),
			EventID:          row.ID,
			ClusterID:        row.ClusterID,
			ClusterName:      row.ClusterName,
			NodeID:           row.NodeID,
			HostID:           row.HostID,
			Hostname:         row.Hostname,
			IP:               row.IP,
			EventType:        row.EventType,
			Severity:         rule.Severity,
			Status:           status,
			RuleKey:          rule.RuleKey,
			RuleName:         rule.RuleName,
			ProcessName:      row.ProcessName,
			PID:              row.PID,
			Role:             row.Role,
			Details:          row.Details,
			CreatedAt:        row.CreatedAt,
			LatestActionNote: "",
		}
		if state != nil {
			alert.AcknowledgedBy = state.AcknowledgedBy
			alert.AcknowledgedAt = state.AcknowledgedAt
			alert.SilencedBy = state.SilencedBy
			alert.SilencedUntil = state.SilencedUntil
			alert.LatestActionNote = state.Note
		}

		result.Alerts = append(result.Alerts, alert)
		switch status {
		case AlertStatusAcknowledged:
			result.Stats.Acknowledged++
		case AlertStatusSilenced:
			result.Stats.Silenced++
		default:
			result.Stats.Firing++
		}
	}

	// 当前实现中，状态过滤在服务层执行，因此 total 以返回结果为准。
	// Status filtering is applied in service layer, so total follows returned rows.
	if filter.Status != "" {
		result.Total = int64(len(result.Alerts))
	}

	return result, nil
}

// AcknowledgeAlert marks one alert event as acknowledged.
// AcknowledgeAlert 将单条告警标记为已确认。
func (s *Service) AcknowledgeAlert(ctx context.Context, eventID uint, operator, note string) (*AlertActionResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if eventID == 0 {
		return nil, fmt.Errorf("invalid event id")
	}
	if strings.TrimSpace(operator) == "" {
		operator = "unknown"
	}

	event, err := s.monitorService.GetEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if !isCriticalEventType(event.EventType) {
		return nil, fmt.Errorf("event is not alertable")
	}

	state, err := s.repo.GetEventStateByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &AlertEventState{EventID: eventID}
	}

	now := time.Now().UTC()
	state.ClusterID = event.ClusterID
	state.Status = AlertStatusAcknowledged
	state.AcknowledgedBy = operator
	state.AcknowledgedAt = &now
	state.SilencedBy = ""
	state.SilencedUntil = nil
	state.Note = note

	if err := s.repo.SaveEventState(ctx, state); err != nil {
		return nil, err
	}
	return toAlertActionResult(state), nil
}

// SilenceAlert silences one alert event for a duration in minutes.
// SilenceAlert 将单条告警静默一段分钟数。
func (s *Service) SilenceAlert(ctx context.Context, eventID uint, operator string, durationMinutes int, note string) (*AlertActionResult, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if eventID == 0 {
		return nil, fmt.Errorf("invalid event id")
	}
	if durationMinutes < 1 || durationMinutes > 7*24*60 {
		return nil, fmt.Errorf("duration_minutes must be between 1 and 10080")
	}
	if strings.TrimSpace(operator) == "" {
		operator = "unknown"
	}

	event, err := s.monitorService.GetEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if !isCriticalEventType(event.EventType) {
		return nil, fmt.Errorf("event is not alertable")
	}

	state, err := s.repo.GetEventStateByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &AlertEventState{EventID: eventID}
	}

	now := time.Now().UTC()
	silencedUntil := now.Add(time.Duration(durationMinutes) * time.Minute)
	state.ClusterID = event.ClusterID
	state.Status = AlertStatusSilenced
	state.SilencedBy = operator
	state.SilencedUntil = &silencedUntil
	state.Note = note

	if err := s.repo.SaveEventState(ctx, state); err != nil {
		return nil, err
	}
	return toAlertActionResult(state), nil
}

// ListClusterRules returns rules for one cluster.
// ListClusterRules 返回单集群告警规则列表。
func (s *Service) ListClusterRules(ctx context.Context, clusterID uint) (*AlertRuleListData, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if clusterID == 0 {
		return nil, fmt.Errorf("invalid cluster id")
	}
	if _, err := s.clusterService.Get(ctx, clusterID); err != nil {
		return nil, err
	}

	if err := s.ensureDefaultRules(ctx, clusterID); err != nil {
		return nil, err
	}

	rules, err := s.repo.ListRulesByClusterID(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	items := make([]*AlertRuleDTO, 0, len(rules))
	for _, rule := range rules {
		items = append(items, toAlertRuleDTO(rule))
	}

	return &AlertRuleListData{
		GeneratedAt: time.Now().UTC(),
		ClusterID:   clusterID,
		Rules:       items,
	}, nil
}

// UpdateClusterRule updates one rule in cluster scope.
// UpdateClusterRule 更新集群维度的一条告警规则。
func (s *Service) UpdateClusterRule(ctx context.Context, clusterID, ruleID uint, req *UpdateAlertRuleRequest) (*AlertRuleDTO, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if clusterID == 0 || ruleID == 0 {
		return nil, fmt.Errorf("invalid cluster id or rule id")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.clusterService.Get(ctx, clusterID); err != nil {
		return nil, err
	}

	rule, err := s.repo.GetRuleByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	if rule.ClusterID != clusterID {
		return nil, fmt.Errorf("rule does not belong to cluster")
	}

	if req.RuleName != nil {
		rule.RuleName = strings.TrimSpace(*req.RuleName)
	}
	if req.Description != nil {
		rule.Description = strings.TrimSpace(*req.Description)
	}
	if req.Severity != nil {
		rule.Severity = *req.Severity
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Threshold != nil {
		rule.Threshold = *req.Threshold
	}
	if req.WindowSeconds != nil {
		rule.WindowSeconds = *req.WindowSeconds
	}

	if err := s.repo.SaveRule(ctx, rule); err != nil {
		return nil, err
	}

	return toAlertRuleDTO(rule), nil
}

// GetIntegrationStatus returns integration health status of monitoring stack.
// GetIntegrationStatus 返回监控栈集成状态。
func (s *Service) GetIntegrationStatus(ctx context.Context) (*IntegrationStatusData, error) {
	obsCfg := config.Config.Observability

	// Keep Prometheus managed targets in sync when entering integration page (best effort).
	// 进入集成页时顺带同步 Prometheus 目标（尽力而为）。
	if obsCfg.Enabled && obsCfg.AutoOnboardClusters && obsCfg.Prometheus.ManageConfig {
		if err := s.SyncManagedClusterTargets(ctx); err != nil {
			logger.WarnF(ctx, "[Monitoring] sync managed cluster targets failed: %v", err)
		}
	}

	components := []*IntegrationComponentStatus{
		{Name: "prometheus", URL: joinURL(obsCfg.Prometheus.URL, "/-/healthy")},
		{Name: "alertmanager", URL: joinURL(obsCfg.Alertmanager.URL, "/-/healthy")},
		{Name: "grafana", URL: joinURL(obsCfg.Grafana.URL, "/api/health")},
	}

	if !obsCfg.Enabled {
		for _, component := range components {
			component.Healthy = false
			component.Error = "disabled by config: observability.enabled=false"
		}

		metricsURL := obsCfg.SeatunnelMetric.Path
		if !strings.HasPrefix(metricsURL, "/") {
			metricsURL = "/" + metricsURL
		}
		components = append(components, &IntegrationComponentStatus{
			Name:    "seatunnel_metrics",
			URL:     metricsURL,
			Healthy: false,
			Error:   "disabled by config: observability.enabled=false",
		})

		return &IntegrationStatusData{
			GeneratedAt: time.Now().UTC(),
			Components:  components,
		}, nil
	}

	for _, component := range components {
		component.Healthy, component.StatusCode, component.Error = checkHTTPComponent(ctx, component.URL, 3*time.Second)
	}

	targets, err := s.collectManagedMetricsTargets(ctx, true)
	metricsComponent := &IntegrationComponentStatus{Name: "seatunnel_metrics"}
	if err != nil {
		metricsComponent.Healthy = false
		metricsComponent.Error = err.Error()
	} else {
		total := len(targets)
		healthy := 0
		for _, target := range targets {
			if metricsComponent.URL == "" {
				metricsComponent.URL = target.ProbeURL
				metricsComponent.StatusCode = target.StatusCode
			}
			if target.Healthy {
				healthy++
				if metricsComponent.StatusCode == 0 {
					metricsComponent.StatusCode = target.StatusCode
				}
				if metricsComponent.URL == "" {
					metricsComponent.URL = target.ProbeURL
				}
			}
		}

		switch {
		case total == 0:
			metricsComponent.Healthy = false
			metricsComponent.Error = "no managed cluster node exposes api_port for metrics probing"
			if metricsComponent.URL == "" {
				metricsComponent.URL = obsCfg.SeatunnelMetric.Path
			}
		case healthy == 0:
			metricsComponent.Healthy = false
			metricsComponent.Error = fmt.Sprintf("metrics endpoint unreachable or metrics disabled on %d/%d targets", total, total)
		case healthy < total:
			metricsComponent.Healthy = true
			metricsComponent.Error = fmt.Sprintf("partial healthy: %d/%d targets expose metrics", healthy, total)
		default:
			metricsComponent.Healthy = true
		}
	}
	components = append(components, metricsComponent)

	return &IntegrationStatusData{
		GeneratedAt: time.Now().UTC(),
		Components:  components,
	}, nil
}

// SyncManagedClusterTargets discovers managed cluster metrics endpoints and rewrites Prometheus scrape config.
// SyncManagedClusterTargets 发现受管集群 metrics 端点并更新 Prometheus 抓取配置。
func (s *Service) SyncManagedClusterTargets(ctx context.Context) error {
	obsCfg := config.Config.Observability
	if !obsCfg.Enabled || !obsCfg.AutoOnboardClusters || !obsCfg.Prometheus.ManageConfig {
		return nil
	}

	configFile := strings.TrimSpace(obsCfg.Prometheus.ConfigFile)
	if configFile == "" {
		return fmt.Errorf("observability.prometheus.config_file is empty while manage_config=true")
	}

	targets, err := s.collectManagedMetricsTargets(ctx, true)
	if err != nil {
		return err
	}

	rendered, err := s.renderPrometheusConfig(targets)
	if err != nil {
		return err
	}

	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		return err
	}

	oldContent, _ := os.ReadFile(configFile)
	if bytes.Equal(oldContent, rendered) {
		return nil
	}

	if err := os.WriteFile(configFile, rendered, 0o644); err != nil {
		return err
	}

	if err := s.reloadPrometheus(ctx); err != nil {
		return err
	}

	logger.InfoF(ctx, "[Monitoring] synced Prometheus managed targets: file=%s, target_count=%d", configFile, len(targets))
	return nil
}

type managedMetricsTarget struct {
	ClusterID   uint
	ClusterName string
	Env         string
	Target      string
	ProbeURL    string
	StatusCode  int
	Healthy     bool
	ProbeError  string
}

func (s *Service) collectManagedMetricsTargets(ctx context.Context, doProbe bool) ([]*managedMetricsTarget, error) {
	obsCfg := config.Config.Observability
	clusters, _, err := s.clusterService.List(ctx, &cluster.ClusterFilter{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	results := make([]*managedMetricsTarget, 0, 16)
	seen := make(map[string]struct{})

	for _, c := range clusters {
		nodes, err := s.clusterService.GetNodes(ctx, c.ID)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if node == nil || node.APIPort <= 0 {
				continue
			}
			if !node.IsOnline {
				continue
			}
			host := strings.TrimSpace(node.HostIP)
			if host == "" {
				continue
			}

			target := net.JoinHostPort(host, strconv.Itoa(node.APIPort))
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}

			item := &managedMetricsTarget{
				ClusterID:   c.ID,
				ClusterName: strings.TrimSpace(c.Name),
				Env:         resolveClusterEnvLabel(c),
				Target:      target,
			}
			if doProbe {
				item.ProbeURL, item.StatusCode, item.Healthy, item.ProbeError = s.probeMetricsEndpoint(ctx, target)
			} else {
				item.ProbeURL = "http://" + target + ensureLeadingSlash(obsCfg.SeatunnelMetric.Path)
			}
			results = append(results, item)
		}
	}

	// Append static targets defined in config for unmanaged/legacy clusters.
	// 追加配置中的静态目标，用于未纳管或历史集群。
	for _, rawTarget := range obsCfg.SeatunnelMetric.StaticTargets {
		target := strings.TrimSpace(rawTarget)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}

		item := &managedMetricsTarget{
			ClusterID:   0,
			ClusterName: "static",
			Env:         "static",
			Target:      target,
		}
		if doProbe {
			item.ProbeURL, item.StatusCode, item.Healthy, item.ProbeError = s.probeMetricsEndpoint(ctx, target)
		} else {
			item.ProbeURL = "http://" + target + ensureLeadingSlash(obsCfg.SeatunnelMetric.Path)
		}
		results = append(results, item)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].ClusterID != results[j].ClusterID {
			return results[i].ClusterID < results[j].ClusterID
		}
		return results[i].Target < results[j].Target
	})
	return results, nil
}

func (s *Service) probeMetricsEndpoint(ctx context.Context, target string) (probeURL string, statusCode int, healthy bool, probeErr string) {
	obsCfg := config.Config.Observability
	path := ensureLeadingSlash(obsCfg.SeatunnelMetric.Path)
	probeURL = "http://" + target + path

	timeout := time.Duration(obsCfg.SeatunnelMetric.ProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return probeURL, 0, false, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeURL, 0, false, err.Error()
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return probeURL, statusCode, false, fmt.Sprintf("http status %d", resp.StatusCode)
	}

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	body := string(payload)
	if strings.Contains(body, "# HELP") ||
		strings.Contains(body, "# TYPE") ||
		strings.Contains(body, "node_state") ||
		strings.Contains(body, "jvm_") {
		return probeURL, statusCode, true, ""
	}
	return probeURL, statusCode, false, "metrics payload signature not detected"
}

type prometheusFileConfig struct {
	Global        prometheusGlobalConfig  `yaml:"global"`
	RuleFiles     []string                `yaml:"rule_files,omitempty"`
	Alerting      prometheusAlertingBlock `yaml:"alerting"`
	ScrapeConfigs []prometheusScrapeJob   `yaml:"scrape_configs"`
}

type prometheusGlobalConfig struct {
	ScrapeInterval     string `yaml:"scrape_interval"`
	EvaluationInterval string `yaml:"evaluation_interval"`
}

type prometheusAlertingBlock struct {
	Alertmanagers []prometheusAlertmanagerConfig `yaml:"alertmanagers"`
}

type prometheusAlertmanagerConfig struct {
	StaticConfigs []prometheusStaticConfig `yaml:"static_configs"`
}

type prometheusScrapeJob struct {
	JobName     string                   `yaml:"job_name"`
	MetricsPath string                   `yaml:"metrics_path,omitempty"`
	Static      []prometheusStaticConfig `yaml:"static_configs"`
}

type prometheusStaticConfig struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels,omitempty"`
}

func (s *Service) renderPrometheusConfig(targets []*managedMetricsTarget) ([]byte, error) {
	obsCfg := config.Config.Observability
	promCfg := obsCfg.Prometheus

	prometheusTarget := resolveURLHost(promCfg.URL, "127.0.0.1:9090")
	alertmanagerTarget := strings.TrimSpace(promCfg.AlertmanagerTarget)
	if alertmanagerTarget == "" {
		alertmanagerTarget = resolveURLHost(obsCfg.Alertmanager.URL, "127.0.0.1:9093")
	}

	fileCfg := &prometheusFileConfig{
		Global: prometheusGlobalConfig{
			ScrapeInterval:     promCfg.ScrapeInterval,
			EvaluationInterval: promCfg.EvaluationInterval,
		},
		RuleFiles: []string{promCfg.RulesGlob},
		Alerting: prometheusAlertingBlock{
			Alertmanagers: []prometheusAlertmanagerConfig{
				{
					StaticConfigs: []prometheusStaticConfig{
						{Targets: []string{alertmanagerTarget}},
					},
				},
			},
		},
		ScrapeConfigs: []prometheusScrapeJob{
			{
				JobName: "prometheus",
				Static: []prometheusStaticConfig{
					{Targets: []string{prometheusTarget}},
				},
			},
			{
				JobName: "alertmanager",
				Static: []prometheusStaticConfig{
					{Targets: []string{alertmanagerTarget}},
				},
			},
		},
	}

	clusterStatics := make(map[uint]*prometheusStaticConfig)
	clusterNames := make(map[uint]string)
	clusterIDs := make([]uint, 0, 8)
	for _, target := range targets {
		if target == nil || !target.Healthy {
			continue
		}
		staticCfg, ok := clusterStatics[target.ClusterID]
		if !ok {
			staticCfg = &prometheusStaticConfig{
				Targets: make([]string, 0, 2),
				Labels: map[string]string{
					"cluster_id":   fmt.Sprintf("%d", target.ClusterID),
					"cluster_name": target.ClusterName,
					"service":      "seatunnel-engine",
				},
			}
			clusterStatics[target.ClusterID] = staticCfg
			clusterNames[target.ClusterID] = target.ClusterName
			clusterIDs = append(clusterIDs, target.ClusterID)
		}
		staticCfg.Targets = append(staticCfg.Targets, target.Target)
	}

	if len(clusterIDs) > 0 {
		sort.Slice(clusterIDs, func(i, j int) bool {
			return clusterIDs[i] < clusterIDs[j]
		})

		seatunnelJob := prometheusScrapeJob{
			JobName:     "seatunnel_engine_http",
			MetricsPath: ensureLeadingSlash(obsCfg.SeatunnelMetric.Path),
			Static:      make([]prometheusStaticConfig, 0, len(clusterIDs)),
		}

		for _, clusterID := range clusterIDs {
			staticCfg := clusterStatics[clusterID]
			sort.Strings(staticCfg.Targets)
			if staticCfg.Labels["cluster_name"] == "" {
				staticCfg.Labels["cluster_name"] = clusterNames[clusterID]
			}
			seatunnelJob.Static = append(seatunnelJob.Static, *staticCfg)
		}

		fileCfg.ScrapeConfigs = append(fileCfg.ScrapeConfigs, seatunnelJob)
	}

	out, err := yaml.Marshal(fileCfg)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

func (s *Service) reloadPrometheus(ctx context.Context) error {
	reloadURL := normalizeBaseURL(config.Config.Observability.Prometheus.ReloadURL)
	if reloadURL == "" {
		return nil
	}
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("prometheus reload failed, status=%d, body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func checkHTTPComponent(ctx context.Context, targetURL string, timeout time.Duration) (bool, int, string) {
	targetURL = normalizeBaseURL(targetURL)
	if targetURL == "" {
		return false, 0, "url is empty"
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false, 0, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err.Error()
	}
	defer resp.Body.Close()

	code := resp.StatusCode
	if code >= 200 && code < 400 {
		return true, code, ""
	}
	return false, code, fmt.Sprintf("http status %d", code)
}

func joinURL(baseURL, suffix string) string {
	base := normalizeBaseURL(baseURL)
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

func normalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	return strings.TrimRight(value, "/")
}

func ensureLeadingSlash(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/metrics"
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return "/" + trimmed
}

func resolveURLHost(rawURL, fallback string) string {
	base := normalizeBaseURL(rawURL)
	if base == "" {
		return fallback
	}
	parsed, err := neturl.Parse(base)
	if err != nil {
		return fallback
	}
	if parsed.Host == "" {
		return fallback
	}
	return parsed.Host
}

// ListNotificationChannels returns all notification channels.
// ListNotificationChannels 返回全部通知渠道。
func (s *Service) ListNotificationChannels(ctx context.Context) (*NotificationChannelListData, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	channels, err := s.repo.ListNotificationChannels(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*NotificationChannelDTO, 0, len(channels))
	for _, channel := range channels {
		items = append(items, toNotificationChannelDTO(channel))
	}

	return &NotificationChannelListData{
		GeneratedAt: time.Now().UTC(),
		Total:       len(items),
		Channels:    items,
	}, nil
}

// CreateNotificationChannel creates one notification channel.
// CreateNotificationChannel 创建一条通知渠道。
func (s *Service) CreateNotificationChannel(ctx context.Context, req *UpsertNotificationChannelRequest) (*NotificationChannelDTO, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	channel := &NotificationChannel{
		Name:        strings.TrimSpace(req.Name),
		Type:        req.Type,
		Enabled:     enabled,
		Endpoint:    strings.TrimSpace(req.Endpoint),
		Secret:      strings.TrimSpace(req.Secret),
		Description: strings.TrimSpace(req.Description),
	}

	if err := s.repo.CreateNotificationChannel(ctx, channel); err != nil {
		return nil, err
	}
	return toNotificationChannelDTO(channel), nil
}

// UpdateNotificationChannel updates one notification channel.
// UpdateNotificationChannel 更新一条通知渠道。
func (s *Service) UpdateNotificationChannel(ctx context.Context, id uint, req *UpsertNotificationChannelRequest) (*NotificationChannelDTO, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("monitoring repository is not configured")
	}
	if id == 0 {
		return nil, fmt.Errorf("invalid channel id")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	channel, err := s.repo.GetNotificationChannelByID(ctx, id)
	if err != nil {
		return nil, err
	}

	channel.Name = strings.TrimSpace(req.Name)
	channel.Type = req.Type
	channel.Endpoint = strings.TrimSpace(req.Endpoint)
	channel.Secret = strings.TrimSpace(req.Secret)
	channel.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		channel.Enabled = *req.Enabled
	}

	if err := s.repo.SaveNotificationChannel(ctx, channel); err != nil {
		return nil, err
	}
	return toNotificationChannelDTO(channel), nil
}

// DeleteNotificationChannel deletes one notification channel.
// DeleteNotificationChannel 删除一条通知渠道。
func (s *Service) DeleteNotificationChannel(ctx context.Context, id uint) error {
	if s.repo == nil {
		return fmt.Errorf("monitoring repository is not configured")
	}
	if id == 0 {
		return fmt.Errorf("invalid channel id")
	}
	return s.repo.DeleteNotificationChannel(ctx, id)
}

func toEventStats(m map[monitor.ProcessEventType]int64) *EventStats {
	if m == nil {
		m = map[monitor.ProcessEventType]int64{}
	}
	return &EventStats{
		Started:             m[monitor.EventTypeStarted],
		Stopped:             m[monitor.EventTypeStopped],
		Crashed:             m[monitor.EventTypeCrashed],
		Restarted:           m[monitor.EventTypeRestarted],
		RestartFailed:       m[monitor.EventTypeRestartFailed],
		RestartLimitReached: m[monitor.EventTypeRestartLimitReached],
	}
}

func mergeEventStats(dst, src *EventStats) {
	if dst == nil || src == nil {
		return
	}
	dst.Started += src.Started
	dst.Stopped += src.Stopped
	dst.Crashed += src.Crashed
	dst.Restarted += src.Restarted
	dst.RestartFailed += src.RestartFailed
	dst.RestartLimitReached += src.RestartLimitReached
}

func isCriticalEventType(eventType monitor.ProcessEventType) bool {
	switch eventType {
	case monitor.EventTypeCrashed, monitor.EventTypeRestartFailed, monitor.EventTypeRestartLimitReached:
		return true
	default:
		return false
	}
}

func eventTypeToRuleKey(eventType monitor.ProcessEventType) string {
	switch eventType {
	case monitor.EventTypeCrashed:
		return AlertRuleKeyProcessCrashed
	case monitor.EventTypeRestartFailed:
		return AlertRuleKeyProcessRestartFailed
	case monitor.EventTypeRestartLimitReached:
		return AlertRuleKeyProcessRestartLimitReached
	default:
		return ""
	}
}

func resolveAlertStatus(state *AlertEventState, now time.Time) AlertStatus {
	if state == nil {
		return AlertStatusFiring
	}

	switch state.Status {
	case AlertStatusSilenced:
		if state.SilencedUntil != nil && state.SilencedUntil.After(now) {
			return AlertStatusSilenced
		}
		return AlertStatusFiring
	case AlertStatusAcknowledged:
		return AlertStatusAcknowledged
	default:
		return AlertStatusFiring
	}
}

func toAlertActionResult(state *AlertEventState) *AlertActionResult {
	if state == nil {
		return nil
	}
	return &AlertActionResult{
		EventID:          state.EventID,
		Status:           state.Status,
		AcknowledgedBy:   state.AcknowledgedBy,
		AcknowledgedAt:   state.AcknowledgedAt,
		SilencedBy:       state.SilencedBy,
		SilencedUntil:    state.SilencedUntil,
		LatestActionNote: state.Note,
	}
}

func toAlertRuleDTO(rule *AlertRule) *AlertRuleDTO {
	if rule == nil {
		return nil
	}
	return &AlertRuleDTO{
		ID:            rule.ID,
		ClusterID:     rule.ClusterID,
		RuleKey:       rule.RuleKey,
		RuleName:      rule.RuleName,
		Description:   rule.Description,
		Severity:      rule.Severity,
		Enabled:       rule.Enabled,
		Threshold:     rule.Threshold,
		WindowSeconds: rule.WindowSeconds,
		CreatedAt:     rule.CreatedAt,
		UpdatedAt:     rule.UpdatedAt,
	}
}

func toNotificationChannelDTO(channel *NotificationChannel) *NotificationChannelDTO {
	if channel == nil {
		return nil
	}
	return &NotificationChannelDTO{
		ID:          channel.ID,
		Name:        channel.Name,
		Type:        channel.Type,
		Enabled:     channel.Enabled,
		Endpoint:    channel.Endpoint,
		Secret:      channel.Secret,
		Description: channel.Description,
		CreatedAt:   channel.CreatedAt,
		UpdatedAt:   channel.UpdatedAt,
	}
}

func (s *Service) ensureDefaultRules(ctx context.Context, clusterID uint) error {
	for _, tpl := range defaultClusterRules(clusterID) {
		_, err := s.repo.GetRuleByClusterAndKey(ctx, clusterID, tpl.RuleKey)
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if createErr := s.repo.CreateRule(ctx, tpl); createErr != nil {
			if isDuplicateKeyError(createErr) {
				continue
			}
			return createErr
		}
	}
	return nil
}

func (s *Service) getClusterRuleMap(ctx context.Context, clusterID uint, cache map[uint]map[string]*AlertRule) (map[string]*AlertRule, error) {
	if ruleMap, ok := cache[clusterID]; ok {
		return ruleMap, nil
	}
	if err := s.ensureDefaultRules(ctx, clusterID); err != nil {
		return nil, err
	}

	rules, err := s.repo.ListRulesByClusterID(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	ruleMap := make(map[string]*AlertRule, len(rules))
	for _, rule := range rules {
		ruleMap[rule.RuleKey] = rule
	}
	cache[clusterID] = ruleMap
	return ruleMap, nil
}

func defaultClusterRules(clusterID uint) []*AlertRule {
	return []*AlertRule{
		{
			ClusterID:     clusterID,
			RuleKey:       AlertRuleKeyProcessCrashed,
			RuleName:      "进程崩溃告警",
			Description:   "当检测到进程崩溃事件时触发告警",
			Severity:      AlertSeverityCritical,
			Enabled:       true,
			Threshold:     1,
			WindowSeconds: 300,
		},
		{
			ClusterID:     clusterID,
			RuleKey:       AlertRuleKeyProcessRestartFailed,
			RuleName:      "进程重启失败告警",
			Description:   "当自动重启失败时触发告警",
			Severity:      AlertSeverityCritical,
			Enabled:       true,
			Threshold:     1,
			WindowSeconds: 300,
		},
		{
			ClusterID:     clusterID,
			RuleKey:       AlertRuleKeyProcessRestartLimitReached,
			RuleName:      "达到重启上限告警",
			Description:   "当进程达到重启上限时触发告警",
			Severity:      AlertSeverityCritical,
			Enabled:       true,
			Threshold:     1,
			WindowSeconds: 300,
		},
	}
}

func defaultRuleByKey(clusterID uint, ruleKey string) *AlertRule {
	for _, rule := range defaultClusterRules(clusterID) {
		if rule.RuleKey == ruleKey {
			return rule
		}
	}
	return nil
}

func resolveClusterEnvLabel(c *cluster.Cluster) string {
	if c == nil || c.Config == nil {
		return "unknown"
	}
	for _, key := range []string{"env", "environment"} {
		if raw, ok := c.Config[key]; ok && raw != nil {
			value := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if value != "" {
				return value
			}
		}
	}
	return "unknown"
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint")
}

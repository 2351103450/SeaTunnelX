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

'use client';

import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import Link from 'next/link';
import {ArrowLeft, Download, ExternalLink, Loader2, Package} from 'lucide-react';
import {toast} from 'sonner';
import services from '@/lib/services';
import type {
  DiagnosticsInspectionFinding,
  DiagnosticsInspectionFindingSeverity,
  DiagnosticsInspectionReport,
  DiagnosticsTask,
  DiagnosticsTaskNodeScope,
  DiagnosticsTaskOptions,
} from '@/lib/services/diagnostics';
import {Badge} from '@/components/ui/badge';
import {Button} from '@/components/ui/button';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {Skeleton} from '@/components/ui/skeleton';
import {Input} from '@/components/ui/input';
import {Label} from '@/components/ui/label';
import {Switch} from '@/components/ui/switch';
import {localizeDiagnosticsText} from './text-utils';

interface InspectionDetailPageProps {
  inspectionId: number;
}

function formatDateTime(value?: string | null): string {
  if (!value) {
    return '-';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function getSeverityLabel(severity: DiagnosticsInspectionFindingSeverity): string {
  switch (severity) {
    case 'critical':
      return '严重';
    case 'warning':
      return '警告';
    case 'info':
      return '信息';
    default:
      return severity;
  }
}

function getSeverityBadgeClass(severity: DiagnosticsInspectionFindingSeverity): string {
  switch (severity) {
    case 'critical':
      return 'bg-red-100 text-red-800 border-red-200';
    case 'warning':
      return 'bg-yellow-100 text-yellow-800 border-yellow-200';
    case 'info':
      return 'bg-blue-100 text-blue-800 border-blue-200';
    default:
      return '';
  }
}

function getStatusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return '等待中';
    case 'running':
      return '执行中';
    case 'completed':
      return '已完成';
    case 'failed':
      return '失败';
    default:
      return status;
  }
}

function getStatusVariant(
  status: string,
): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (status) {
    case 'completed':
    case 'succeeded':
      return 'default';
    case 'failed':
      return 'destructive';
    case 'running':
      return 'secondary';
    default:
      return 'outline';
  }
}

function getTriggerSourceLabel(source: string): string {
  switch (source) {
    case 'manual':
      return '手动触发';
    case 'auto':
      return '自动触发';
    case 'cluster_detail':
      return '集群详情';
    case 'diagnostics_workspace':
      return '诊断工作台';
    default:
      return source;
  }
}

function getFindingSeverityScore(
  severity: DiagnosticsInspectionFindingSeverity,
): number {
  switch (severity) {
    case 'critical':
      return 3;
    case 'warning':
      return 2;
    case 'info':
    default:
      return 1;
  }
}

const DEFAULT_BUNDLE_OPTIONS: DiagnosticsTaskOptions = {
  include_thread_dump: true,
  include_jvm_dump: false,
  jvm_dump_min_free_mb: 2048,
  log_sample_lines: 200,
};

export default function InspectionDetailPage({
  inspectionId,
}: InspectionDetailPageProps) {
  const [loading, setLoading] = useState(true);
  const [report, setReport] = useState<DiagnosticsInspectionReport | null>(null);
  const [findings, setFindings] = useState<DiagnosticsInspectionFinding[]>([]);
  const [creatingBundle, setCreatingBundle] = useState(false);
  const [bundleTask, setBundleTask] = useState<DiagnosticsTask | null>(null);
  const [pollingBundle, setPollingBundle] = useState(false);
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [bundleOptions, setBundleOptions] =
    useState<DiagnosticsTaskOptions>(DEFAULT_BUNDLE_OPTIONS);
  const [nodeScope, setNodeScope] =
    useState<DiagnosticsTaskNodeScope>('all');

  const pollBundleTask = useCallback(
    async (taskId: number) => {
      const result = await services.diagnostics.getTaskSafe(taskId);
      if (!result.success || !result.data) {
        return;
      }
      setBundleTask(result.data);
      const status = result.data.status;
      if (
        status === 'succeeded' ||
        status === 'failed' ||
        status === 'cancelled'
      ) {
        setPollingBundle(false);
        if (pollTimerRef.current) {
          clearInterval(pollTimerRef.current);
          pollTimerRef.current = null;
        }
      }
    },
    [],
  );

  const loadDetail = useCallback(async () => {
    setLoading(true);
    try {
      const result =
        await services.diagnostics.getInspectionReportDetailSafe(inspectionId);
      if (!result.success || !result.data) {
        toast.error(result.error || '加载巡检详情失败');
        setReport(null);
        setFindings([]);
        setBundleTask(null);
        return;
      }
      setReport(result.data.report);
      setFindings(result.data.findings || []);
      const related = result.data.related_diagnostic_task;
      if (related) {
        setBundleTask(related);
        const status = related.status;
        if (
          status !== 'succeeded' &&
          status !== 'failed' &&
          status !== 'cancelled'
        ) {
          if (pollTimerRef.current) {
            clearInterval(pollTimerRef.current);
            pollTimerRef.current = null;
          }
          setPollingBundle(true);
          const taskId = related.id;
          pollTimerRef.current = setInterval(() => {
            void pollBundleTask(taskId);
          }, 3000);
        }
      } else {
        setBundleTask(null);
      }
    } finally {
      setLoading(false);
    }
  }, [inspectionId, pollBundleTask]);

  useEffect(() => {
    void loadDetail();
  }, [loadDetail]);

  // Sort findings by severity: critical > warning > info
  const sortedFindings = useMemo(
    () =>
      [...findings].sort(
        (a, b) =>
          getFindingSeverityScore(b.severity) -
          getFindingSeverityScore(a.severity),
      ),
    [findings],
  );

  useEffect(() => {
    return () => {
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current);
      }
    };
  }, []);

  const handleCreateBundle = useCallback(async () => {
    if (!report) {
      return;
    }
    // Pick the first finding (prefer critical > warning > info) to satisfy
    // backend validation: inspection_finding trigger requires inspection_finding_id.
    const firstFinding =
      findings.find((f) => f.severity === 'critical') ??
      findings.find((f) => f.severity === 'warning') ??
      findings[0];

    setCreatingBundle(true);
    try {
      const payloadScope: DiagnosticsTaskNodeScope = nodeScope || 'all';
      const result = await services.diagnostics.createTaskSafe({
        cluster_id: report.cluster_id,
        trigger_source: firstFinding ? 'inspection_finding' : 'manual',
        source_ref: firstFinding
          ? {
              inspection_report_id: report.id,
              inspection_finding_id: firstFinding.id,
            }
          : undefined,
        node_scope: payloadScope,
        options: bundleOptions,
        auto_start: true,
      });
      if (!result.success || !result.data) {
        toast.error(result.error || '创建诊断包失败');
        return;
      }
      toast.success('诊断包创建成功，正在执行...');
      setBundleTask(result.data);
      setPollingBundle(true);
      // Start polling
      const taskId = result.data.id;
      pollTimerRef.current = setInterval(() => {
        void pollBundleTask(taskId);
      }, 3000);
    } finally {
      setCreatingBundle(false);
    }
  }, [bundleOptions, pollBundleTask, report]);

  if (loading) {
    return (
      <div className='space-y-4'>
        <Skeleton className='h-10 w-48' />
        <Skeleton className='h-32 w-full' />
        <Skeleton className='h-64 w-full' />
      </div>
    );
  }

  if (!report) {
    return (
      <div className='space-y-4'>
        <Button asChild variant='ghost'>
          <Link href='/diagnostics?tab=inspections'>
            <ArrowLeft className='mr-2 h-4 w-4' />
            返回巡检列表
          </Link>
        </Button>
        <Card>
          <CardContent className='py-8 text-center text-muted-foreground'>
            巡检报告不存在或加载失败
          </CardContent>
        </Card>
      </div>
    );
  }

  const isCompleted = report.status === 'completed';
  const hasFindings = findings.length > 0;

  return (
    <div className='space-y-4'>
      {/* Header */}
      <div className='flex items-center gap-3'>
        <Button asChild variant='ghost' size='sm'>
          <Link href='/diagnostics?tab=inspections'>
            <ArrowLeft className='mr-2 h-4 w-4' />
            返回巡检列表
          </Link>
        </Button>
        <h1 className='text-2xl font-bold tracking-tight'>巡检详情</h1>
        <Badge variant='outline'>#{report.id}</Badge>
      </div>

      {/* Status Banner */}
      <Card>
        <CardContent className='space-y-3 pt-6'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge variant={getStatusVariant(report.status)}>
              {getStatusLabel(report.status)}
            </Badge>
            <Badge variant='outline'>
              {getTriggerSourceLabel(report.trigger_source)}
            </Badge>
            {report.cluster_name ? (
              <Badge variant='outline'>{report.cluster_name}</Badge>
            ) : (
              <Badge variant='outline'>集群 #{report.cluster_id}</Badge>
            )}
          </div>
          {report.trigger_source === 'auto' && report.auto_trigger_reason ? (
            <div className='rounded-md border border-yellow-200 bg-yellow-50 px-3 py-2 text-sm text-yellow-800'>
              自动触发原因：{report.auto_trigger_reason}
            </div>
          ) : null}
          <div className='grid gap-2 text-sm sm:grid-cols-2 lg:grid-cols-4'>
            <div>
              <span className='text-muted-foreground'>创建时间：</span>
              {formatDateTime(report.created_at)}
            </div>
            <div>
              <span className='text-muted-foreground'>完成时间：</span>
              {formatDateTime(report.finished_at)}
            </div>
            <div>
              <span className='text-muted-foreground'>回溯时间：</span>
              {report.lookback_minutes || 30} 分钟
            </div>
            <div>
              <span className='text-muted-foreground'>发起人：</span>
              {report.requested_by || '-'}
            </div>
          </div>
          {report.summary ? (
            <div className='text-sm'>
              {localizeDiagnosticsText(report.summary)}
            </div>
          ) : null}
          {report.error_message ? (
            <div className='rounded-md border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive'>
              {report.error_message}
            </div>
          ) : null}
          <div className='text-sm text-muted-foreground'>
            发现统计：共 {report.finding_total} 条（严重{' '}
            {report.critical_count} / 警告 {report.warning_count} / 信息{' '}
            {report.info_count}）
          </div>
        </CardContent>
      </Card>

      {/* Findings Section */}
      <Card>
        <CardHeader>
          <CardTitle>巡检发现</CardTitle>
        </CardHeader>
        <CardContent>
          {sortedFindings.length === 0 ? (
            <div className='flex items-center justify-center rounded-lg border border-dashed p-8 text-sm text-muted-foreground'>
              暂无巡检发现
            </div>
          ) : (
            <div className='space-y-4'>
              {sortedFindings.map((finding) => (
                <div
                  key={finding.id}
                  className='rounded-lg border p-4 space-y-3'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <Badge
                      variant='outline'
                      className={getSeverityBadgeClass(finding.severity)}
                    >
                      {getSeverityLabel(finding.severity)}
                    </Badge>
                    <Badge variant='outline'>{finding.category}</Badge>
                    <Badge variant='outline'>{finding.check_code}</Badge>
                  </div>
                  <div className='font-medium'>
                    {localizeDiagnosticsText(
                      finding.check_name || finding.summary,
                    )}
                  </div>
                  <div className='text-sm text-muted-foreground'>
                    {localizeDiagnosticsText(finding.summary)}
                  </div>
                  {finding.evidence_summary ? (
                    <div className='rounded-md bg-muted/40 p-3 text-sm text-muted-foreground'>
                      {localizeDiagnosticsText(finding.evidence_summary)}
                    </div>
                  ) : null}
                  {finding.recommendation ? (
                    <div className='text-sm text-muted-foreground'>
                      建议：{localizeDiagnosticsText(finding.recommendation)}
                    </div>
                  ) : null}
                  {finding.related_error_group_id > 0 ? (
                    <Button asChild size='sm' variant='outline'>
                      <Link
                        href={`/diagnostics?tab=errors&cluster_id=${finding.cluster_id}&group_id=${finding.related_error_group_id}&source=inspection-finding`}
                      >
                        查看错误组 &rarr;
                      </Link>
                    </Button>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Diagnostic Bundle Section */}
      {isCompleted ? (
        <Card>
          <CardHeader>
            <CardTitle>诊断包</CardTitle>
          </CardHeader>
          <CardContent>
            {bundleTask ? (
              <div className='space-y-4'>
                <div className='flex flex-wrap items-center gap-2'>
                  <Badge variant={getStatusVariant(bundleTask.status)}>
                    {getStatusLabel(bundleTask.status)}
                  </Badge>
                  <Badge variant='outline'>任务 #{bundleTask.id}</Badge>
                  {pollingBundle ? (
                    <span className='flex items-center gap-1 text-xs text-muted-foreground'>
                      <Loader2 className='h-3 w-3 animate-spin' />
                      正在刷新...
                    </span>
                  ) : null}
                </div>

                {/* Step progress */}
                {Array.isArray(bundleTask.steps) &&
                bundleTask.steps.length > 0 ? (
                  <div className='space-y-2'>
                    <div className='text-sm font-medium'>执行步骤</div>
                    <div className='space-y-1'>
                      {bundleTask.steps.map((step) => (
                        <div
                          key={step.id}
                          className='flex items-center gap-2 rounded-md border px-3 py-2 text-sm'
                        >
                          <Badge
                            variant={getStatusVariant(step.status)}
                            className='text-xs'
                          >
                            {getStatusLabel(step.status)}
                          </Badge>
                          <span className='font-mono text-xs text-muted-foreground'>
                            {step.code}
                          </span>
                          <span className='flex-1 truncate'>
                            {localizeDiagnosticsText(step.title) ||
                              step.description}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                ) : null}

                {/* Download / View links when succeeded */}
                {bundleTask.status === 'succeeded' ? (
                  <div className='flex flex-wrap gap-2'>
                    <Button asChild variant='outline' size='sm'>
                      <a
                        href={services.diagnostics.getTaskHTMLUrl(
                          bundleTask.id,
                        )}
                        target='_blank'
                        rel='noopener noreferrer'
                      >
                        <ExternalLink className='mr-2 h-4 w-4' />
                        查看 HTML 报告
                      </a>
                    </Button>
                    <Button asChild variant='outline' size='sm'>
                      <a
                        href={services.diagnostics.getTaskBundleUrl(
                          bundleTask.id,
                        )}
                        download
                      >
                        <Download className='mr-2 h-4 w-4' />
                        下载诊断包
                      </a>
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void handleCreateBundle()}
                      disabled={creatingBundle}
                    >
                      {creatingBundle ? (
                        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                      ) : (
                        <Package className='mr-2 h-4 w-4' />
                      )}
                      重新生成诊断包
                    </Button>
                  </div>
                ) : null}

                {bundleTask.status === 'failed' ? (
                  <div className='space-y-2'>
                    <div className='rounded-md border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive'>
                      {bundleTask.failure_reason || '诊断包生成失败'}
                    </div>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void handleCreateBundle()}
                      disabled={creatingBundle}
                    >
                      {creatingBundle ? (
                        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                      ) : (
                        <Package className='mr-2 h-4 w-4' />
                      )}
                      重新生成诊断包
                    </Button>
                  </div>
                ) : null}
              </div>
            ) : hasFindings ? (
              <div className='space-y-4'>
                <p className='text-sm text-muted-foreground'>
                  巡检已完成，发现 {findings.length}{' '}
                  条问题。生成诊断包时，你可以选择节点范围、是否采集线程 / JVM Dump 以及日志采样范围。
                </p>

                <div className='space-y-3 rounded-lg border bg-background p-3'>
                  <div className='text-sm font-medium'>节点范围</div>
                  <div className='flex flex-wrap gap-2 text-xs'>
                    <Button
                      type='button'
                      variant={nodeScope === 'all' ? 'default' : 'outline'}
                      size='sm'
                      onClick={() => setNodeScope('all')}
                    >
                      全部节点
                    </Button>
                    <Button
                      type='button'
                      variant={nodeScope === 'related' ? 'default' : 'outline'}
                      size='sm'
                      onClick={() => setNodeScope('related')}
                    >
                      仅问题相关节点
                    </Button>
                    <Button
                      type='button'
                      variant={nodeScope === 'custom' ? 'default' : 'outline'}
                      size='sm'
                      disabled
                    >
                      自定义节点列表（即将支持）
                    </Button>
                  </div>
                  <p className='text-xs text-muted-foreground'>
                    默认会对集群内所有受管节点采集线程 / 日志等信息；选择「仅问题相关节点」时，将只针对当前巡检发现关联的节点。
                  </p>
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='flex items-center justify-between rounded-lg border bg-background p-3'>
                    <div>
                      <div className='font-medium'>采集线程 Dump</div>
                      <div className='text-xs text-muted-foreground'>
                        建议保持开启，用于分析线程状态、死锁等问题。
                      </div>
                    </div>
                    <Switch
                      checked={bundleOptions.include_thread_dump}
                      onCheckedChange={(checked) =>
                        setBundleOptions((current) => ({
                          ...current,
                          include_thread_dump: checked,
                        }))
                      }
                    />
                  </div>

                  <div className='flex items-center justify-between rounded-lg border bg-background p-3'>
                    <div>
                      <div className='font-medium'>采集 JVM Dump</div>
                      <div className='text-xs text-muted-foreground'>
                        仅在需要深入分析内存问题时开启，生成文件较大。
                      </div>
                    </div>
                    <Switch
                      checked={bundleOptions.include_jvm_dump}
                      onCheckedChange={(checked) =>
                        setBundleOptions((current) => ({
                          ...current,
                          include_jvm_dump: checked,
                        }))
                      }
                    />
                  </div>

                  <div className='space-y-2'>
                    <Label htmlFor='inspection-bundle-log-lines'>
                      日志采样行数
                    </Label>
                    <Input
                      id='inspection-bundle-log-lines'
                      type='number'
                      min={50}
                      step={50}
                      value={
                        bundleOptions.log_sample_lines ??
                        DEFAULT_BUNDLE_OPTIONS.log_sample_lines
                      }
                      onChange={(event) =>
                        setBundleOptions((current) => ({
                          ...current,
                          log_sample_lines:
                            Number.parseInt(event.target.value, 10) ||
                            DEFAULT_BUNDLE_OPTIONS.log_sample_lines,
                        }))
                      }
                    />
                    <div className='text-xs text-muted-foreground'>
                      控制诊断包中每个关键步骤采样的日志行数，行数越多信息越完整，体积也会更大。
                    </div>
                  </div>

                  <div className='space-y-2'>
                    <Label htmlFor='inspection-bundle-jvm-space'>
                      JVM Dump 最小剩余内存（MB）
                    </Label>
                    <Input
                      id='inspection-bundle-jvm-space'
                      type='number'
                      min={256}
                      step={256}
                      value={
                        bundleOptions.jvm_dump_min_free_mb ??
                        DEFAULT_BUNDLE_OPTIONS.jvm_dump_min_free_mb
                      }
                      onChange={(event) =>
                        setBundleOptions((current) => ({
                          ...current,
                          jvm_dump_min_free_mb:
                            Number.parseInt(event.target.value, 10) ||
                            DEFAULT_BUNDLE_OPTIONS.jvm_dump_min_free_mb,
                        }))
                      }
                      disabled={!bundleOptions.include_jvm_dump}
                    />
                    <div className='text-xs text-muted-foreground'>
                      仅在开启 JVM Dump 时生效，用于避免在可用空间过小的机器上生成 Dump。
                    </div>
                  </div>
                </div>

                <Button
                  onClick={() => void handleCreateBundle()}
                  disabled={creatingBundle}
                >
                  {creatingBundle ? (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  ) : (
                    <Package className='mr-2 h-4 w-4' />
                  )}
                  生成诊断包
                </Button>
              </div>
            ) : (
              <div className='text-sm text-muted-foreground'>
                巡检已完成，未发现问题，无需生成诊断包。
              </div>
            )}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

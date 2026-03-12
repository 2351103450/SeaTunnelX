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

import {useCallback, useEffect, useState} from 'react';
import {Loader2, Plus, Trash2} from 'lucide-react';
import {toast} from 'sonner';
import services from '@/lib/services';
import type {
  InspectionAutoPolicy,
  InspectionConditionItem,
  InspectionConditionTemplate,
  DiagnosticsClusterOption,
  DiagnosticsTaskOptions,
} from '@/lib/services/diagnostics';
import {Badge} from '@/components/ui/badge';
import {Button} from '@/components/ui/button';
import {Checkbox} from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {Input} from '@/components/ui/input';
import {Label} from '@/components/ui/label';
import {Switch} from '@/components/ui/switch';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

interface AutoPolicyConfigPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  clusterOptions: DiagnosticsClusterOption[];
}

const CATEGORY_LABELS: Record<string, string> = {
  java_error: 'Java 错误',
  prometheus: 'Prometheus 指标',
  error_rate: '错误频率',
  node_unhealthy: '节点异常',
  alert_firing: '告警触发',
  schedule: '定时巡检',
};

export function AutoPolicyConfigPanel({
  open,
  onOpenChange,
  clusterOptions,
}: AutoPolicyConfigPanelProps) {
  const [policies, setPolicies] = useState<InspectionAutoPolicy[]>([]);
  const [templates, setTemplates] = useState<InspectionConditionTemplate[]>([]);
  const [loading, setLoading] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<InspectionAutoPolicy | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);

  // Form state
  const [formName, setFormName] = useState('');
  const [formClusterId, setFormClusterId] = useState(0);
  const [formEnabled, setFormEnabled] = useState(true);
  const [formCooldown, setFormCooldown] = useState(30);
  const [formConditions, setFormConditions] = useState<InspectionConditionItem[]>([]);
  const [formAutoCreateTask, setFormAutoCreateTask] = useState(false);
  const [formAutoStartTask, setFormAutoStartTask] = useState(true);
  const [formTaskOptions, setFormTaskOptions] = useState<DiagnosticsTaskOptions>({
    include_thread_dump: true,
    include_jvm_dump: false,
    jvm_dump_min_free_mb: 2048,
    log_sample_lines: 200,
  });

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [policiesResult, templatesResult] = await Promise.all([
        services.diagnostics.listAutoPoliciesSafe({page_size: 100}),
        services.diagnostics.listBuiltinConditionTemplatesSafe(),
      ]);
      if (policiesResult.success && policiesResult.data) {
        setPolicies(policiesResult.data.items || []);
      } else {
        toast.error(policiesResult.error || '加载策略列表失败');
      }
      if (templatesResult.success && templatesResult.data) {
        setTemplates(templatesResult.data);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) {
      void loadData();
    }
  }, [loadData, open]);

  const openCreateForm = useCallback(() => {
    setEditingPolicy(null);
    setFormName('');
    setFormClusterId(0);
    setFormEnabled(true);
    setFormCooldown(30);
    setFormConditions([]);
    setFormAutoCreateTask(false);
    setFormAutoStartTask(true);
    setFormTaskOptions({
      include_thread_dump: true,
      include_jvm_dump: false,
      jvm_dump_min_free_mb: 2048,
      log_sample_lines: 200,
    });
    setFormOpen(true);
  }, []);

  const openEditForm = useCallback(
    (policy: InspectionAutoPolicy) => {
      setEditingPolicy(policy);
      setFormName(policy.name);
      setFormClusterId(policy.cluster_id);
      setFormEnabled(policy.enabled);
      setFormCooldown(policy.cooldown_minutes);
      setFormConditions(policy.conditions || []);
      setFormAutoCreateTask(policy.auto_create_task);
      setFormAutoStartTask(policy.auto_start_task);
      setFormTaskOptions(
        policy.task_options || {
          include_thread_dump: true,
          include_jvm_dump: false,
          jvm_dump_min_free_mb: 2048,
          log_sample_lines: 200,
        },
      );
      setFormOpen(true);
    },
    [],
  );

  const handleToggleCondition = useCallback(
    (templateCode: string, checked: boolean) => {
      setFormConditions((prev) => {
        if (checked) {
          if (prev.some((c) => c.template_code === templateCode)) {
            return prev.map((c) =>
              c.template_code === templateCode
                ? {...c, enabled: true}
                : c,
            );
          }
          return [...prev, {template_code: templateCode, enabled: true}];
        }
        return prev.filter((c) => c.template_code !== templateCode);
      });
    },
    [],
  );

  const handleConditionOverride = useCallback(
    (
      templateCode: string,
      field: 'threshold_override' | 'window_minutes_override',
      value: number | null,
    ) => {
      setFormConditions((prev) =>
        prev.map((c) =>
          c.template_code === templateCode ? {...c, [field]: value} : c,
        ),
      );
    },
    [],
  );

  const handleSave = useCallback(async () => {
    if (!formName.trim()) {
      toast.error('请输入策略名称');
      return;
    }
    setSaving(true);
    try {
      if (editingPolicy) {
        const result = await services.diagnostics.updateAutoPolicySafe(
          editingPolicy.id,
          {
            name: formName,
            enabled: formEnabled,
            conditions: formConditions,
            cooldown_minutes: formCooldown,
            auto_create_task: formAutoCreateTask,
            auto_start_task: formAutoStartTask,
            task_options: formAutoCreateTask ? formTaskOptions : undefined,
          },
        );
        if (!result.success) {
          toast.error(result.error || '更新策略失败');
          return;
        }
        toast.success('策略已更新');
      } else {
        const result = await services.diagnostics.createAutoPolicySafe({
          cluster_id: formClusterId,
          name: formName,
          enabled: formEnabled,
          conditions: formConditions,
          cooldown_minutes: formCooldown,
          auto_create_task: formAutoCreateTask,
          auto_start_task: formAutoStartTask,
          task_options: formAutoCreateTask ? formTaskOptions : undefined,
        });
        if (!result.success) {
          toast.error(result.error || '创建策略失败');
          return;
        }
        toast.success('策略已创建');
      }
      setFormOpen(false);
      void loadData();
    } finally {
      setSaving(false);
    }
  }, [
    editingPolicy,
    formClusterId,
    formConditions,
    formCooldown,
    formEnabled,
    formName,
    loadData,
  ]);

  const handleDelete = useCallback(
    async (id: number) => {
      setDeleting(id);
      try {
        const result = await services.diagnostics.deleteAutoPolicySafe(id);
        if (!result.success) {
          toast.error(result.error || '删除策略失败');
          return;
        }
        toast.success('策略已删除');
        void loadData();
      } finally {
        setDeleting(null);
      }
    },
    [loadData],
  );

  const handleToggleEnabled = useCallback(
    async (policy: InspectionAutoPolicy) => {
      const result = await services.diagnostics.updateAutoPolicySafe(
        policy.id,
        {enabled: !policy.enabled},
      );
      if (!result.success) {
        toast.error(result.error || '更新策略失败');
        return;
      }
      void loadData();
    },
    [loadData],
  );

  // Group templates by category
  const groupedTemplates = templates.reduce<
    Record<string, InspectionConditionTemplate[]>
  >((acc, tpl) => {
    const key = tpl.category;
    if (!acc[key]) {
      acc[key] = [];
    }
    acc[key].push(tpl);
    return acc;
  }, {});

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='max-w-2xl max-h-[80vh] overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>自动巡检策略</DialogTitle>
            <DialogDescription>
              配置自动巡检触发条件，满足条件时自动发起巡检。
            </DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className='flex items-center justify-center py-8'>
              <Loader2 className='h-6 w-6 animate-spin text-muted-foreground' />
            </div>
          ) : (
            <div className='space-y-4'>
              <div className='flex items-center justify-between'>
                <div className='text-sm text-muted-foreground'>
                  共 {policies.length} 条策略
                </div>
                <Button size='sm' onClick={openCreateForm}>
                  <Plus className='mr-2 h-4 w-4' />
                  新建策略
                </Button>
              </div>

              {policies.length === 0 ? (
                <div className='flex items-center justify-center rounded-lg border border-dashed p-8 text-sm text-muted-foreground'>
                  暂无自动巡检策略
                </div>
              ) : (
                <div className='space-y-3'>
                  {policies.map((policy) => (
                    <div
                      key={policy.id}
                      className='flex items-center gap-3 rounded-lg border p-4'
                    >
                      <div className='flex-1 min-w-0'>
                        <div className='flex items-center gap-2'>
                          <span className='font-medium truncate'>
                            {policy.name}
                          </span>
                          <Badge variant='outline'>
                            {policy.cluster_id === 0
                              ? '全局'
                              : `集群 #${policy.cluster_id}`}
                          </Badge>
                        </div>
                        <div className='mt-1 text-xs text-muted-foreground space-y-1'>
                          <div>
                            条件数：{(policy.conditions || []).length} | 冷却：
                            {policy.cooldown_minutes} 分钟
                          </div>
                          {policy.auto_create_task ? (
                            <div>
                              自动诊断包：
                              {policy.auto_start_task ? '自动创建并执行' : '自动创建，手动执行'}
                            </div>
                          ) : null}
                        </div>
                      </div>
                      <Switch
                        checked={policy.enabled}
                        onCheckedChange={() =>
                          void handleToggleEnabled(policy)
                        }
                      />
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => openEditForm(policy)}
                      >
                        编辑
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => void handleDelete(policy.id)}
                        disabled={deleting === policy.id}
                      >
                        {deleting === policy.id ? (
                          <Loader2 className='h-4 w-4 animate-spin' />
                        ) : (
                          <Trash2 className='h-4 w-4 text-destructive' />
                        )}
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Create/Edit form dialog */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className='max-w-2xl max-h-[85vh] overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>
              {editingPolicy ? '编辑策略' : '新建策略'}
            </DialogTitle>
          </DialogHeader>

          <div className='space-y-4'>
            <div className='space-y-2'>
              <Label>策略名称</Label>
              <Input
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder='例如：Java OOM 自动巡检'
              />
            </div>

            {!editingPolicy ? (
              <div className='space-y-2'>
                <Label>适用集群</Label>
                <Select
                  value={String(formClusterId)}
                  onValueChange={(value) =>
                    setFormClusterId(Number.parseInt(value, 10) || 0)
                  }
                >
                  <SelectTrigger>
                    <SelectValue placeholder='请选择适用集群' />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='0'>全局策略（所有集群）</SelectItem>
                    {clusterOptions.map((cluster) => (
                      <SelectItem
                        key={cluster.cluster_id}
                        value={String(cluster.cluster_id)}
                      >
                        {cluster.cluster_name}（#{cluster.cluster_id}）
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className='text-xs text-muted-foreground'>
                  选择「全局策略」表示对所有集群生效；选择具体集群则仅对该集群生效。
                </div>
              </div>
            ) : null}

            <div className='space-y-2'>
              <Label>冷却时间（分钟）</Label>
              <Input
                type='number'
                min={1}
                max={1440}
                value={formCooldown}
                onChange={(e) =>
                  setFormCooldown(
                    Number.parseInt(e.target.value, 10) || 30,
                  )
                }
              />
              <div className='text-xs text-muted-foreground'>
                同一策略触发后在冷却时间内不会重复触发
              </div>
            </div>

            <div className='flex items-center gap-2'>
              <Switch
                checked={formEnabled}
                onCheckedChange={setFormEnabled}
              />
              <Label>启用策略</Label>
            </div>

            <div className='space-y-3 rounded-lg border bg-muted/10 p-4'>
              <div className='flex items-center justify-between'>
                <div>
                  <div className='font-medium'>自动生成诊断包和报告</div>
                  <div className='text-xs text-muted-foreground'>
                    命中该策略并触发巡检后，自动创建一次诊断任务，用于收集线程栈、JVM Dump 和日志样本。
                  </div>
                </div>
                <Switch
                  checked={formAutoCreateTask}
                  onCheckedChange={setFormAutoCreateTask}
                />
              </div>

              {formAutoCreateTask ? (
                <div className='mt-3 grid gap-4 md:grid-cols-2'>
                  <div className='flex items-center justify-between rounded-lg border bg-background p-3'>
                    <div>
                      <div className='font-medium'>采集线程 Dump</div>
                      <div className='text-xs text-muted-foreground'>
                        建议保持开启，用于分析线程状态和潜在死锁。
                      </div>
                    </div>
                    <Switch
                      checked={formTaskOptions.include_thread_dump}
                      onCheckedChange={(checked) =>
                        setFormTaskOptions((current: DiagnosticsTaskOptions) => ({
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
                        仅在内存问题（例如 OOM）等场景建议开启，Dump 文件较大。
                      </div>
                    </div>
                    <Switch
                      checked={formTaskOptions.include_jvm_dump}
                      onCheckedChange={(checked) =>
                        setFormTaskOptions((current: DiagnosticsTaskOptions) => ({
                          ...current,
                          include_jvm_dump: checked,
                        }))
                      }
                    />
                  </div>

                  <div className='space-y-2'>
                    <Label htmlFor='auto-policy-log-lines'>日志采样行数</Label>
                    <Input
                      id='auto-policy-log-lines'
                      type='number'
                      min={50}
                      step={50}
                      value={formTaskOptions.log_sample_lines ?? 200}
                      onChange={(event) =>
                        setFormTaskOptions((current: DiagnosticsTaskOptions) => ({
                          ...current,
                          log_sample_lines:
                            Number.parseInt(event.target.value, 10) || 200,
                        }))
                      }
                    />
                    <div className='text-xs text-muted-foreground'>
                      控制每个关键步骤采样的日志行数，行数越多信息越完整，诊断包体积也会更大。
                    </div>
                  </div>

                  <div className='space-y-2'>
                    <Label htmlFor='auto-policy-jvm-space'>
                      JVM Dump 最小剩余内存（MB）
                    </Label>
                    <Input
                      id='auto-policy-jvm-space'
                      type='number'
                      min={256}
                      step={256}
                      value={formTaskOptions.jvm_dump_min_free_mb ?? 2048}
                      onChange={(event) =>
                        setFormTaskOptions((current: DiagnosticsTaskOptions) => ({
                          ...current,
                          jvm_dump_min_free_mb:
                            Number.parseInt(event.target.value, 10) || 2048,
                        }))
                      }
                      disabled={!formTaskOptions.include_jvm_dump}
                    />
                    <div className='text-xs text-muted-foreground'>
                      仅在开启 JVM Dump 时生效，用于避免在可用空间过小的机器上生成 Dump。
                    </div>
                  </div>

                  <div className='flex items-center gap-2 md:col-span-2'>
                    <Switch
                      checked={formAutoStartTask}
                      onCheckedChange={setFormAutoStartTask}
                    />
                    <div className='text-sm text-muted-foreground'>
                      自动开始执行诊断任务（关闭时仅自动创建任务，不会立即运行）
                    </div>
                  </div>
                </div>
              ) : null}
            </div>

            <div className='space-y-3'>
              <Label>触发条件</Label>
              {Object.entries(groupedTemplates).map(
                ([category, categoryTemplates]) => (
                  <div
                    key={category}
                    className='rounded-lg border p-3 space-y-2'
                  >
                    <div className='text-sm font-medium'>
                      {CATEGORY_LABELS[category] || category}
                    </div>
                    {categoryTemplates.map((tpl) => {
                      const isChecked = formConditions.some(
                        (c) => c.template_code === tpl.code,
                      );
                      const condition = formConditions.find(
                        (c) => c.template_code === tpl.code,
                      );
                      return (
                        <div
                          key={tpl.code}
                          className='space-y-2 rounded-md border bg-muted/20 p-2'
                        >
                          <div className='flex items-center gap-2'>
                            <Checkbox
                              checked={isChecked}
                              onCheckedChange={(checked) =>
                                handleToggleCondition(
                                  tpl.code,
                                  checked === true,
                                )
                              }
                            />
                            <div className='flex-1'>
                              <div className='text-sm font-medium'>
                                {tpl.name}
                              </div>
                              <div className='text-xs text-muted-foreground'>
                                {tpl.description}
                              </div>
                            </div>
                          </div>
                          {isChecked && !tpl.immediate_on_match ? (
                            <div className='ml-6 grid grid-cols-2 gap-2'>
                              {tpl.default_threshold > 0 ? (
                                <div className='space-y-1'>
                                  <Label className='text-xs'>
                                    阈值（默认 {tpl.default_threshold}）
                                  </Label>
                                  <Input
                                    type='number'
                                    className='h-8 text-xs'
                                    placeholder={String(
                                      tpl.default_threshold,
                                    )}
                                    value={
                                      condition?.threshold_override ?? ''
                                    }
                                    onChange={(e) =>
                                      handleConditionOverride(
                                        tpl.code,
                                        'threshold_override',
                                        e.target.value
                                          ? Number(e.target.value)
                                          : null,
                                      )
                                    }
                                  />
                                </div>
                              ) : null}
                              {tpl.default_window_minutes > 0 ? (
                                <div className='space-y-1'>
                                  <Label className='text-xs'>
                                    窗口（默认{' '}
                                    {tpl.default_window_minutes} 分钟）
                                  </Label>
                                  <Input
                                    type='number'
                                    className='h-8 text-xs'
                                    placeholder={String(
                                      tpl.default_window_minutes,
                                    )}
                                    value={
                                      condition?.window_minutes_override ??
                                      ''
                                    }
                                    onChange={(e) =>
                                      handleConditionOverride(
                                        tpl.code,
                                        'window_minutes_override',
                                        e.target.value
                                          ? Number(e.target.value)
                                          : null,
                                      )
                                    }
                                  />
                                </div>
                              ) : null}
                            </div>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                ),
              )}
              {templates.length === 0 ? (
                <div className='text-sm text-muted-foreground'>
                  暂无可用的条件模板
                </div>
              ) : null}
            </div>
          </div>

          <DialogFooter>
            <Button variant='outline' onClick={() => setFormOpen(false)}>
              取消
            </Button>
            <Button onClick={() => void handleSave()} disabled={saving}>
              {saving ? (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              ) : null}
              {editingPolicy ? '保存修改' : '创建策略'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

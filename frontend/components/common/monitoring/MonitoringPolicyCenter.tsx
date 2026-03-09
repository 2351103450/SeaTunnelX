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

import {useCallback, useEffect, useMemo, useState} from 'react';
import {useTranslations} from 'next-intl';
import {toast} from 'sonner';
import {
  BellRing,
  History,
  Mail,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Send,
  Trash2,
  Webhook,
  X,
} from 'lucide-react';
import services from '@/lib/services';
import type {ClusterInfo} from '@/lib/services/cluster';
import type {
  AlertPolicy,
  AlertPolicyBuilderKind,
  AlertPolicyCenterBootstrapData,
  AlertPolicyListData,
  AlertPolicyTemplateSummary,
  AlertSeverity,
  NotificationChannel,
  NotificationChannelEmailConfig,
  NotificationDelivery,
  NotificationDeliveryListData,
  UpsertAlertPolicyRequest,
  UpsertNotificationChannelRequest,
} from '@/lib/services/monitoring';
import {cn} from '@/lib/utils';
import {Badge} from '@/components/ui/badge';
import {Button} from '@/components/ui/button';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {Input} from '@/components/ui/input';
import {Label} from '@/components/ui/label';
import {Separator} from '@/components/ui/separator';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {Switch} from '@/components/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {Textarea} from '@/components/ui/textarea';

type StrategyEditorMode = 'static' | 'custom_promql';

type PolicyFormState = {
  name: string;
  description: string;
  strategyMode: StrategyEditorMode;
  templateKey: string;
  clusterId: string;
  severity: AlertSeverity;
  cooldownMinutes: string;
  sendRecovery: boolean;
  enabled: boolean;
  promql: string;
  emailChannelId: string;
  webhookChannelId: string;
};

type EmailChannelFormState = {
  id: number | null;
  name: string;
  enabled: boolean;
  description: string;
  protocol: string;
  security: 'none' | 'starttls' | 'ssl';
  host: string;
  port: string;
  username: string;
  password: string;
  from: string;
  recipients: string;
};

const EMPTY_BOOTSTRAP: AlertPolicyCenterBootstrapData = {
  generated_at: '',
  capability_mode: '',
  capabilities: [],
  builders: [],
  templates: [],
  components: [],
};

const EMPTY_HISTORY: NotificationDeliveryListData = {
  generated_at: '',
  page: 1,
  page_size: 10,
  total: 0,
  deliveries: [],
};

function createDefaultPolicyForm(): PolicyFormState {
  return {
    name: '',
    description: '',
    strategyMode: 'static',
    templateKey: '',
    clusterId: '',
    severity: 'warning',
    cooldownMinutes: '1',
    sendRecovery: true,
    enabled: true,
    promql: '',
    emailChannelId: 'none',
    webhookChannelId: 'none',
  };
}

function createDefaultEmailChannelForm(): EmailChannelFormState {
  return {
    id: null,
    name: '',
    enabled: true,
    description: '',
    protocol: 'smtp',
    security: 'ssl',
    host: '',
    port: '465',
    username: '',
    password: '',
    from: '',
    recipients: '',
  };
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

function resolveSeverityVariant(
  severity: AlertSeverity,
): 'secondary' | 'destructive' {
  return severity === 'critical' ? 'destructive' : 'secondary';
}

function resolveDeliveryStatusVariant(
  status?: string,
): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (status) {
    case 'sent':
      return 'default';
    case 'failed':
      return 'destructive';
    case 'sending':
    case 'retrying':
      return 'secondary';
    default:
      return 'outline';
  }
}

function normalizePolicies(data?: AlertPolicyListData): AlertPolicy[] {
  if (!data || !Array.isArray(data.policies)) {
    return [];
  }
  return data.policies;
}

function normalizeChannels(
  channels: NotificationChannel[],
): NotificationChannel[] {
  return Array.isArray(channels) ? channels : [];
}

function notificationMethodSummary(
  policy: AlertPolicy,
  channelMap: Map<number, NotificationChannel>,
): string {
  const items = (policy.notification_channel_ids || [])
    .map((id) => channelMap.get(id))
    .filter(Boolean)
    .map((channel) => channel?.name || '')
    .filter(Boolean);
  return items.length > 0 ? items.join(' / ') : '-';
}

function parseRecipients(value: string): string[] {
  return value
    .split(/[;,\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function getEmailConfig(
  channel?: NotificationChannel | null,
): NotificationChannelEmailConfig | null {
  if (!channel?.config?.email) {
    return null;
  }
  return channel.config.email;
}

function createEmailChannelFormFromChannel(
  channel: NotificationChannel,
): EmailChannelFormState {
  const config = getEmailConfig(channel);
  return {
    id: channel.id,
    name: channel.name,
    enabled: channel.enabled,
    description: channel.description || '',
    protocol: config?.protocol || 'smtp',
    security: config?.security || 'ssl',
    host: config?.host || '',
    port: String(config?.port || 465),
    username: config?.username || '',
    password: config?.password || '',
    from: config?.from || '',
    recipients: (config?.recipients || []).join(', '),
  };
}

function createPolicyFormFromPolicy(
  policy: AlertPolicy,
  channels: NotificationChannel[],
): PolicyFormState {
  const form = createDefaultPolicyForm();
  const selectedChannels = (policy.notification_channel_ids || [])
    .map((id) => channels.find((channel) => channel.id === id))
    .filter(Boolean) as NotificationChannel[];
  const selectedEmailChannel =
    selectedChannels.find((channel) => channel.type === 'email') || null;
  const selectedWebhookChannel =
    selectedChannels.find((channel) => channel.type === 'webhook') || null;

  return {
    ...form,
    name: policy.name || '',
    description: policy.description || '',
    strategyMode:
      policy.policy_type === 'custom_promql' ? 'custom_promql' : 'static',
    templateKey: policy.template_key || '',
    clusterId: policy.cluster_id || '',
    severity: policy.severity,
    cooldownMinutes: String(policy.cooldown_minutes ?? 0),
    sendRecovery: policy.send_recovery,
    enabled: policy.enabled,
    promql: policy.promql || '',
    emailChannelId: selectedEmailChannel
      ? String(selectedEmailChannel.id)
      : 'none',
    webhookChannelId: selectedWebhookChannel
      ? String(selectedWebhookChannel.id)
      : 'none',
  };
}

function buildPolicyPayload(
  form: PolicyFormState,
  selectedTemplate: AlertPolicyTemplateSummary | null,
): UpsertAlertPolicyRequest {
  const notificationChannelIds = [form.emailChannelId, form.webhookChannelId]
    .filter((value) => value !== 'none')
    .map((value) => Number.parseInt(value, 10))
    .filter((value) => Number.isFinite(value));

  const policyType: AlertPolicyBuilderKind =
    form.strategyMode === 'custom_promql'
      ? 'custom_promql'
      : (selectedTemplate?.source_kind as AlertPolicyBuilderKind) ||
        'platform_health';

  return {
    name: form.name.trim(),
    description: form.description.trim(),
    policy_type: policyType,
    template_key:
      form.strategyMode === 'custom_promql' ? undefined : form.templateKey,
    legacy_rule_key:
      form.strategyMode === 'custom_promql'
        ? undefined
        : selectedTemplate?.legacy_rule_key || undefined,
    cluster_id: form.clusterId,
    severity: form.severity,
    enabled: form.enabled,
    cooldown_minutes: Number.parseInt(form.cooldownMinutes, 10) || 0,
    send_recovery: form.sendRecovery,
    promql:
      form.strategyMode === 'custom_promql' ? form.promql.trim() : undefined,
    notification_channel_ids: notificationChannelIds,
  };
}

function getPolicyExecutionStatusLabel(
  t: ReturnType<typeof useTranslations>,
  status?: string,
): string {
  switch (status) {
    case 'sent':
      return t('history.statuses.sent');
    case 'failed':
      return t('history.statuses.failed');
    case 'partial':
      return t('executionStatuses.partial');
    case 'matched':
      return t('executionStatuses.matched');
    default:
      return t('executionStatuses.idle');
  }
}

function getTemplateTranslation(
  t: ReturnType<typeof useTranslations>,
  templateKey: string,
  field: 'name' | 'description',
): string | null {
  if (!templateKey) {
    return null;
  }

  try {
    return t(`templates.${templateKey}.${field}` as never);
  } catch {
    return null;
  }
}

function getTemplateDisplayName(
  template: Pick<AlertPolicyTemplateSummary, 'key' | 'name'> | null | undefined,
  t: ReturnType<typeof useTranslations>,
): string {
  if (!template) {
    return '-';
  }

  return (
    getTemplateTranslation(t, template.key, 'name') ||
    template.name ||
    template.key
  );
}

function getTemplateDescription(
  template:
    | Pick<AlertPolicyTemplateSummary, 'key' | 'description'>
    | null
    | undefined,
  t: ReturnType<typeof useTranslations>,
): string {
  if (!template) {
    return '';
  }

  return (
    getTemplateTranslation(t, template.key, 'description') ||
    template.description ||
    ''
  );
}

export function MonitoringPolicyCenter() {
  const rootT = useTranslations('monitoringCenter');
  const t = useTranslations('monitoringCenter.policyCenterV2');
  const legacyT = useTranslations('monitoringCenter.policyCenter');

  const [bootstrap, setBootstrap] =
    useState<AlertPolicyCenterBootstrapData>(EMPTY_BOOTSTRAP);
  const [policies, setPolicies] = useState<AlertPolicy[]>([]);
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [clusters, setClusters] = useState<ClusterInfo[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [savingPolicy, setSavingPolicy] = useState<boolean>(false);
  const [editingPolicyId, setEditingPolicyId] = useState<number | null>(null);
  const [deletingPolicyId, setDeletingPolicyId] = useState<number | null>(null);
  const [historyPolicy, setHistoryPolicy] = useState<AlertPolicy | null>(null);
  const [historyOpen, setHistoryOpen] = useState<boolean>(false);
  const [historyData, setHistoryData] =
    useState<NotificationDeliveryListData>(EMPTY_HISTORY);
  const [historyLoading, setHistoryLoading] = useState<boolean>(false);

  const [emailDialogOpen, setEmailDialogOpen] = useState<boolean>(false);
  const [emailChannelForm, setEmailChannelForm] =
    useState<EmailChannelFormState>(createDefaultEmailChannelForm());
  const [savingEmailChannel, setSavingEmailChannel] = useState<boolean>(false);
  const [testingEmailChannelId, setTestingEmailChannelId] = useState<
    number | null
  >(null);
  const [form, setForm] = useState<PolicyFormState>(createDefaultPolicyForm());

  const loadResources = useCallback(async () => {
    setLoading(true);
    try {
      const [bootstrapResult, policiesResult, channelsResult, clustersResult] =
        await Promise.all([
          services.monitoring.getAlertPolicyCenterBootstrapSafe(),
          services.monitoring.listAlertPoliciesSafe(),
          services.monitoring.listNotificationChannelsSafe(),
          services.cluster.getClustersSafe({current: 1, size: 100}),
        ]);

      if (bootstrapResult.success && bootstrapResult.data) {
        setBootstrap(bootstrapResult.data);
      } else {
        toast.error(bootstrapResult.error || legacyT('loadError'));
        setBootstrap(EMPTY_BOOTSTRAP);
      }

      if (policiesResult.success && policiesResult.data) {
        setPolicies(normalizePolicies(policiesResult.data));
      } else {
        toast.error(policiesResult.error || legacyT('policyListLoadError'));
        setPolicies([]);
      }

      if (channelsResult.success && channelsResult.data) {
        setChannels(normalizeChannels(channelsResult.data.channels || []));
      } else {
        toast.error(channelsResult.error || legacyT('channelListLoadError'));
        setChannels([]);
      }

      if (clustersResult.success && clustersResult.data) {
        setClusters(clustersResult.data.clusters || []);
      } else {
        toast.error(clustersResult.error || legacyT('clusterListLoadError'));
        setClusters([]);
      }
    } finally {
      setLoading(false);
    }
  }, [legacyT]);

  useEffect(() => {
    void loadResources();
  }, [loadResources]);

  const channelMap = useMemo(
    () => new Map(channels.map((channel) => [channel.id, channel])),
    [channels],
  );

  const emailChannels = useMemo(
    () => channels.filter((channel) => channel.type === 'email'),
    [channels],
  );
  const webhookChannels = useMemo(
    () => channels.filter((channel) => channel.type === 'webhook'),
    [channels],
  );

  const builderMap = useMemo(() => {
    const map = new Map<string, string>();
    for (const builder of bootstrap.builders || []) {
      map.set(builder.key, builder.status);
    }
    return map;
  }, [bootstrap.builders]);

  const templateGroups = useMemo(() => {
    const platformTemplates = (bootstrap.templates || []).filter(
      (template) => template.source_kind === 'platform_health',
    );
    const metricsTemplates = (bootstrap.templates || []).filter(
      (template) => template.source_kind === 'metrics_template',
    );
    return {
      platformTemplates,
      metricsTemplates,
    };
  }, [bootstrap.templates]);

  const availableStaticTemplates = useMemo(() => {
    const items = [...templateGroups.platformTemplates];
    if (builderMap.get('metrics_template') === 'available') {
      items.push(...templateGroups.metricsTemplates);
    }
    return items;
  }, [builderMap, templateGroups]);

  const selectedTemplate = useMemo(
    () =>
      availableStaticTemplates.find(
        (template) => template.key === form.templateKey,
      ) || null,
    [availableStaticTemplates, form.templateKey],
  );

  const currentEmailChannel = useMemo(
    () =>
      emailChannels.find(
        (channel) => String(channel.id) === form.emailChannelId,
      ) || null,
    [emailChannels, form.emailChannelId],
  );
  const currentWebhookChannel = useMemo(
    () =>
      webhookChannels.find(
        (channel) => String(channel.id) === form.webhookChannelId,
      ) || null,
    [form.webhookChannelId, webhookChannels],
  );

  useEffect(() => {
    if (clusters.length === 0) {
      return;
    }
    setForm((prev) => {
      if (prev.clusterId) {
        return prev;
      }
      return {
        ...prev,
        clusterId: String(clusters[0].id),
      };
    });
  }, [clusters]);

  useEffect(() => {
    if (availableStaticTemplates.length === 0) {
      return;
    }
    setForm((prev) => {
      if (prev.strategyMode !== 'static' || prev.templateKey) {
        return prev;
      }
      return {
        ...prev,
        templateKey: availableStaticTemplates[0].key,
      };
    });
  }, [availableStaticTemplates]);

  const resetPolicyForm = useCallback(() => {
    setEditingPolicyId(null);
    setForm((prev) => ({
      ...createDefaultPolicyForm(),
      clusterId: prev.clusterId || (clusters[0] ? String(clusters[0].id) : ''),
      templateKey:
        availableStaticTemplates[0]?.key ||
        createDefaultPolicyForm().templateKey,
    }));
  }, [availableStaticTemplates, clusters]);

  const handleRefresh = useCallback(async () => {
    await loadResources();
  }, [loadResources]);

  const handleEditPolicy = useCallback(
    (policy: AlertPolicy) => {
      setEditingPolicyId(policy.id);
      setForm(createPolicyFormFromPolicy(policy, channels));
    },
    [channels],
  );

  const handleDeletePolicy = async (policyId: number) => {
    setDeletingPolicyId(policyId);
    try {
      const result = await services.monitoring.deleteAlertPolicySafe(policyId);
      if (!result.success) {
        toast.error(result.error || t('deleteError'));
        return;
      }
      toast.success(t('deleteSuccess'));
      if (editingPolicyId === policyId) {
        resetPolicyForm();
      }
      await loadResources();
    } finally {
      setDeletingPolicyId(null);
    }
  };

  const loadHistory = useCallback(
    async (policy: AlertPolicy) => {
      setHistoryLoading(true);
      try {
        const result = await services.monitoring.listAlertPolicyExecutionsSafe(
          policy.id,
          {page: 1, page_size: 10},
        );
        if (!result.success || !result.data) {
          toast.error(result.error || legacyT('history.loadError'));
          setHistoryData(EMPTY_HISTORY);
          return;
        }
        setHistoryPolicy(policy);
        setHistoryData(result.data);
        setHistoryOpen(true);
      } finally {
        setHistoryLoading(false);
      }
    },
    [legacyT],
  );

  const handleSubmitPolicy = async () => {
    if (!form.name.trim()) {
      toast.error(t('nameRequired'));
      return;
    }
    if (!form.clusterId) {
      toast.error(t('clusterRequired'));
      return;
    }
    if (form.strategyMode === 'static' && !selectedTemplate) {
      toast.error(t('templateRequired'));
      return;
    }
    if (form.strategyMode === 'custom_promql' && !form.promql.trim()) {
      toast.error(t('promqlRequired'));
      return;
    }

    const payload = buildPolicyPayload(form, selectedTemplate);
    if (
      form.strategyMode === 'static' &&
      selectedTemplate?.legacy_rule_key &&
      payload.cluster_id === 'all'
    ) {
      toast.error(t('concreteClusterRequired'));
      return;
    }

    setSavingPolicy(true);
    try {
      const result =
        editingPolicyId === null
          ? await services.monitoring.createAlertPolicySafe(payload)
          : await services.monitoring.updateAlertPolicySafe(
              editingPolicyId,
              payload,
            );
      if (!result.success) {
        toast.error(
          result.error ||
            (editingPolicyId === null ? t('createError') : t('updateError')),
        );
        return;
      }
      toast.success(
        editingPolicyId === null ? t('createSuccess') : t('updateSuccess'),
      );
      await loadResources();
      resetPolicyForm();
    } finally {
      setSavingPolicy(false);
    }
  };

  const openNewEmailChannelDialog = () => {
    setEmailChannelForm(createDefaultEmailChannelForm());
    setEmailDialogOpen(true);
  };

  const handleSaveEmailChannel = async () => {
    if (!emailChannelForm.name.trim()) {
      toast.error(t('channel.nameRequired'));
      return;
    }

    const payload: UpsertNotificationChannelRequest = {
      name: emailChannelForm.name.trim(),
      type: 'email',
      enabled: emailChannelForm.enabled,
      description: emailChannelForm.description.trim(),
      config: {
        email: {
          protocol: emailChannelForm.protocol,
          security: emailChannelForm.security,
          host: emailChannelForm.host.trim(),
          port: Number.parseInt(emailChannelForm.port, 10) || 0,
          username: emailChannelForm.username.trim(),
          password: emailChannelForm.password,
          from: emailChannelForm.from.trim(),
          recipients: parseRecipients(emailChannelForm.recipients),
        },
      },
    };

    setSavingEmailChannel(true);
    try {
      const result =
        emailChannelForm.id === null
          ? await services.monitoring.createNotificationChannelSafe(payload)
          : await services.monitoring.updateNotificationChannelSafe(
              emailChannelForm.id,
              payload,
            );
      if (!result.success || !result.data) {
        toast.error(
          result.error ||
            (emailChannelForm.id === null
              ? t('channel.createError')
              : t('channel.updateError')),
        );
        return;
      }
      toast.success(
        emailChannelForm.id === null
          ? t('channel.createSuccess')
          : t('channel.updateSuccess'),
      );
      const savedChannel = result.data;
      await loadResources();
      setEmailChannelForm(createEmailChannelFormFromChannel(savedChannel));
      setForm((prev) => ({
        ...prev,
        emailChannelId: String(savedChannel.id),
      }));
    } finally {
      setSavingEmailChannel(false);
    }
  };

  const handleTestEmailChannel = async (channelId: number) => {
    setTestingEmailChannelId(channelId);
    try {
      const result =
        await services.monitoring.testNotificationChannelSafe(channelId);
      if (!result.success || !result.data) {
        toast.error(result.error || t('channel.testError'));
        return;
      }
      if (result.data.status === 'sent') {
        toast.success(t('channel.testSuccess'));
      } else {
        toast.error(result.data.last_error || t('channel.testError'));
      }
    } finally {
      setTestingEmailChannelId(null);
    }
  };

  const canShowCustomPromql = builderMap.get('custom_promql') === 'available';

  const policyRows = useMemo(() => policies, [policies]);

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader className='flex flex-col gap-4 md:flex-row md:items-center md:justify-between'>
          <div className='space-y-1'>
            <CardTitle className='flex items-center gap-2'>
              <BellRing className='h-5 w-5 text-primary' />
              {t('title')}
            </CardTitle>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              variant='outline'
              onClick={handleRefresh}
              disabled={loading}
            >
              <RefreshCw className='mr-2 h-4 w-4' />
              {rootT('refresh')}
            </Button>
            <Button variant='outline' onClick={openNewEmailChannelDialog}>
              <Mail className='mr-2 h-4 w-4' />
              {t('channel.manageEmail')}
            </Button>
            <Button onClick={resetPolicyForm}>
              <Plus className='mr-2 h-4 w-4' />
              {t('createNew')}
            </Button>
          </div>
        </CardHeader>
      </Card>

      <div className='grid gap-4 xl:grid-cols-[1.15fr_0.95fr]'>
        <Card>
          <CardHeader>
            <CardTitle>{t('policyListTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('columns.name')}</TableHead>
                  <TableHead>{t('columns.template')}</TableHead>
                  <TableHead>{t('columns.cluster')}</TableHead>
                  <TableHead>{t('columns.severity')}</TableHead>
                  <TableHead>{t('columns.methods')}</TableHead>
                  <TableHead>{t('columns.status')}</TableHead>
                  <TableHead>{t('columns.updatedAt')}</TableHead>
                  <TableHead className='text-right'>
                    {rootT('actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-center text-muted-foreground'
                    >
                      {rootT('loading')}
                    </TableCell>
                  </TableRow>
                ) : policyRows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-center text-muted-foreground'
                    >
                      {t('emptyPolicies')}
                    </TableCell>
                  </TableRow>
                ) : (
                  policyRows.map((policy) => {
                    const template = availableStaticTemplates.find(
                      (item) => item.key === policy.template_key,
                    );
                    const cluster = clusters.find(
                      (item) => String(item.id) === policy.cluster_id,
                    );
                    return (
                      <TableRow key={policy.id}>
                        <TableCell className='font-medium'>
                          <div className='space-y-1'>
                            <div>{policy.name}</div>
                            <div className='text-xs text-muted-foreground'>
                              {policy.enabled ? t('enabled') : t('disabled')}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          {policy.policy_type === 'custom_promql'
                            ? t('customPromql')
                            : template
                              ? getTemplateDisplayName(template, legacyT)
                              : getTemplateTranslation(
                                    legacyT,
                                    policy.template_key || '',
                                    'name',
                                  ) ||
                                  policy.template_key ||
                                  '-'}
                        </TableCell>
                        <TableCell>
                          {cluster?.name || policy.cluster_id || '-'}
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={resolveSeverityVariant(policy.severity)}
                          >
                            {policy.severity === 'critical'
                              ? rootT('alertSeverity.critical')
                              : rootT('alertSeverity.warning')}
                          </Badge>
                        </TableCell>
                        <TableCell className='max-w-[220px] truncate'>
                          {notificationMethodSummary(policy, channelMap)}
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={resolveDeliveryStatusVariant(
                              policy.last_execution_status,
                            )}
                          >
                            {getPolicyExecutionStatusLabel(
                              legacyT,
                              policy.last_execution_status,
                            )}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {formatDateTime(policy.updated_at)}
                        </TableCell>
                        <TableCell>
                          <div className='flex items-center justify-end gap-2'>
                            <Button
                              variant='ghost'
                              size='icon'
                              onClick={() => void loadHistory(policy)}
                              disabled={historyLoading}
                            >
                              <History className='h-4 w-4' />
                            </Button>
                            <Button
                              variant='ghost'
                              size='icon'
                              onClick={() => handleEditPolicy(policy)}
                            >
                              <Pencil className='h-4 w-4' />
                            </Button>
                            <Button
                              variant='ghost'
                              size='icon'
                              onClick={() => void handleDeletePolicy(policy.id)}
                              disabled={deletingPolicyId === policy.id}
                            >
                              <Trash2 className='h-4 w-4 text-destructive' />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>
              {editingPolicyId === null
                ? t('editorCreateTitle')
                : t('editorEditTitle')}
            </CardTitle>
          </CardHeader>
          <CardContent className='space-y-6'>
            <div className='space-y-4'>
              <div className='space-y-2'>
                <Label>{t('fields.name')}</Label>
                <Input
                  value={form.name}
                  onChange={(event) =>
                    setForm((prev) => ({...prev, name: event.target.value}))
                  }
                  placeholder={t('placeholders.name')}
                />
              </div>

              <div className='space-y-2'>
                <Label>{t('fields.description')}</Label>
                <Textarea
                  value={form.description}
                  onChange={(event) =>
                    setForm((prev) => ({
                      ...prev,
                      description: event.target.value,
                    }))
                  }
                  placeholder={t('placeholders.description')}
                  rows={3}
                />
              </div>
            </div>

            <Separator />

            <div className='space-y-4'>
              <div className='space-y-2'>
                <Label>{t('fields.strategyType')}</Label>
                <div className='grid grid-cols-2 gap-2'>
                  <button
                    type='button'
                    className={cn(
                      'rounded-lg border px-4 py-3 text-left text-sm transition-colors',
                      form.strategyMode === 'static'
                        ? 'border-primary bg-primary/5 text-foreground'
                        : 'border-border text-muted-foreground hover:text-foreground',
                    )}
                    onClick={() =>
                      setForm((prev) => ({...prev, strategyMode: 'static'}))
                    }
                  >
                    <div className='font-medium'>{t('staticConfig')}</div>
                    <div className='mt-1 text-xs text-muted-foreground'>
                      {t('staticConfigDesc')}
                    </div>
                  </button>
                  <button
                    type='button'
                    disabled={!canShowCustomPromql}
                    className={cn(
                      'rounded-lg border px-4 py-3 text-left text-sm transition-colors',
                      form.strategyMode === 'custom_promql'
                        ? 'border-primary bg-primary/5 text-foreground'
                        : 'border-border text-muted-foreground hover:text-foreground',
                      !canShowCustomPromql && 'cursor-not-allowed opacity-50',
                    )}
                    onClick={() =>
                      canShowCustomPromql &&
                      setForm((prev) => ({
                        ...prev,
                        strategyMode: 'custom_promql',
                      }))
                    }
                  >
                    <div className='font-medium'>{t('customPromql')}</div>
                    <div className='mt-1 text-xs text-muted-foreground'>
                      {canShowCustomPromql
                        ? t('customPromqlDesc')
                        : t('customPromqlUnavailable')}
                    </div>
                  </button>
                </div>
              </div>

              {form.strategyMode === 'static' ? (
                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label>{t('fields.template')}</Label>
                    <Select
                      value={form.templateKey}
                      onValueChange={(value) =>
                        setForm((prev) => ({...prev, templateKey: value}))
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder={t('templateRequired')} />
                      </SelectTrigger>
                      <SelectContent>
                        {templateGroups.platformTemplates.length > 0 ? (
                          <>
                            <div className='px-2 py-1 text-xs font-medium text-muted-foreground'>
                              {t('groups.platformHealth')}
                            </div>
                            {templateGroups.platformTemplates.map(
                              (template) => (
                                <SelectItem
                                  key={template.key}
                                  value={template.key}
                                >
                                  {getTemplateDisplayName(template, legacyT)}
                                </SelectItem>
                              ),
                            )}
                          </>
                        ) : null}
                        {builderMap.get('metrics_template') === 'available' &&
                        templateGroups.metricsTemplates.length > 0 ? (
                          <>
                            <div className='px-2 py-1 text-xs font-medium text-muted-foreground'>
                              {t('groups.prometheusMetrics')}
                            </div>
                            {templateGroups.metricsTemplates.map((template) => (
                              <SelectItem
                                key={template.key}
                                value={template.key}
                              >
                                {getTemplateDisplayName(template, legacyT)}
                              </SelectItem>
                            ))}
                          </>
                        ) : null}
                      </SelectContent>
                    </Select>
                    {selectedTemplate ? (
                      <p className='text-xs text-muted-foreground'>
                        {getTemplateDescription(selectedTemplate, legacyT)}
                      </p>
                    ) : null}
                  </div>
                  <div className='space-y-2'>
                    <Label>{t('fields.cluster')}</Label>
                    <Select
                      value={form.clusterId}
                      onValueChange={(value) =>
                        setForm((prev) => ({...prev, clusterId: value}))
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder={t('clusterRequired')} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='all'>{t('allClusters')}</SelectItem>
                        {clusters.map((cluster) => (
                          <SelectItem
                            key={cluster.id}
                            value={String(cluster.id)}
                          >
                            {cluster.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {selectedTemplate?.legacy_rule_key ? (
                      <p className='text-xs text-muted-foreground'>
                        {t('concreteClusterHint')}
                      </p>
                    ) : null}
                  </div>
                </div>
              ) : (
                <div className='space-y-2'>
                  <Label>{t('fields.promql')}</Label>
                  <Textarea
                    value={form.promql}
                    onChange={(event) =>
                      setForm((prev) => ({...prev, promql: event.target.value}))
                    }
                    placeholder={t('placeholders.promql')}
                    rows={5}
                  />
                </div>
              )}
            </div>

            <Separator />

            <div className='space-y-4'>
              <div className='grid gap-4 md:grid-cols-3'>
                <div className='space-y-2'>
                  <Label>{t('fields.severity')}</Label>
                  <Select
                    value={form.severity}
                    onValueChange={(value) =>
                      setForm((prev) => ({
                        ...prev,
                        severity: value as AlertSeverity,
                      }))
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value='warning'>
                        {rootT('alertSeverity.warning')}
                      </SelectItem>
                      <SelectItem value='critical'>
                        {rootT('alertSeverity.critical')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-2'>
                  <Label>{t('fields.cooldown')}</Label>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='number'
                      min={0}
                      value={form.cooldownMinutes}
                      onChange={(event) =>
                        setForm((prev) => ({
                          ...prev,
                          cooldownMinutes: event.target.value,
                        }))
                      }
                    />
                    <span className='text-sm text-muted-foreground'>
                      {t('minutes')}
                    </span>
                  </div>
                </div>
                <div className='flex items-end gap-2'>
                  <div className='space-y-2'>
                    <Label>{t('fields.enabled')}</Label>
                    <div>
                      <Switch
                        checked={form.enabled}
                        onCheckedChange={(checked) =>
                          setForm((prev) => ({...prev, enabled: checked}))
                        }
                      />
                    </div>
                  </div>
                </div>
              </div>

              <div className='space-y-2'>
                <Label>{t('fields.methods')}</Label>
                <div className='grid gap-3 md:grid-cols-2'>
                  <button
                    type='button'
                    className={cn(
                      'rounded-lg border px-4 py-3 text-left transition-colors',
                      form.emailChannelId !== 'none'
                        ? 'border-primary bg-primary/5'
                        : 'border-border',
                    )}
                    onClick={() => {
                      if (emailChannels.length === 0) {
                        openNewEmailChannelDialog();
                        return;
                      }
                      setForm((prev) => ({
                        ...prev,
                        emailChannelId:
                          prev.emailChannelId !== 'none'
                            ? 'none'
                            : String(emailChannels[0].id),
                      }));
                    }}
                  >
                    <div className='flex items-center gap-2 font-medium'>
                      <Mail className='h-4 w-4 text-amber-500' />
                      {t('methods.email')}
                    </div>
                    <div className='mt-1 text-xs text-muted-foreground'>
                      {emailChannels.length > 0
                        ? currentEmailChannel?.name || t('methods.selectEmail')
                        : t('methods.noEmailConfig')}
                    </div>
                  </button>
                  <button
                    type='button'
                    disabled={webhookChannels.length === 0}
                    className={cn(
                      'rounded-lg border px-4 py-3 text-left transition-colors',
                      form.webhookChannelId !== 'none'
                        ? 'border-primary bg-primary/5'
                        : 'border-border',
                      webhookChannels.length === 0 &&
                        'cursor-not-allowed opacity-50',
                    )}
                    onClick={() =>
                      webhookChannels.length > 0 &&
                      setForm((prev) => ({
                        ...prev,
                        webhookChannelId:
                          prev.webhookChannelId !== 'none'
                            ? 'none'
                            : String(webhookChannels[0].id),
                      }))
                    }
                  >
                    <div className='flex items-center gap-2 font-medium'>
                      <Webhook className='h-4 w-4 text-sky-500' />
                      {t('methods.webhook')}
                    </div>
                    <div className='mt-1 text-xs text-muted-foreground'>
                      {webhookChannels.length > 0
                        ? currentWebhookChannel?.name ||
                          t('methods.selectWebhook')
                        : t('methods.noWebhookConfig')}
                    </div>
                  </button>
                </div>
              </div>

              {form.emailChannelId !== 'none' ? (
                <div className='space-y-2'>
                  <div className='flex items-center justify-between gap-2'>
                    <Label>{t('fields.emailChannel')}</Label>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={openNewEmailChannelDialog}
                    >
                      {t('channel.manageEmail')}
                    </Button>
                  </div>
                  <Select
                    value={form.emailChannelId}
                    onValueChange={(value) =>
                      setForm((prev) => ({...prev, emailChannelId: value}))
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {emailChannels.map((channel) => (
                        <SelectItem key={channel.id} value={String(channel.id)}>
                          {channel.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {currentEmailChannel ? (
                    <p className='text-xs text-muted-foreground'>
                      {getEmailConfig(currentEmailChannel)?.recipients?.join(
                        ', ',
                      ) || currentEmailChannel.endpoint}
                    </p>
                  ) : null}
                </div>
              ) : null}

              {form.webhookChannelId !== 'none' ? (
                <div className='space-y-2'>
                  <Label>{t('fields.webhookChannel')}</Label>
                  <Select
                    value={form.webhookChannelId}
                    onValueChange={(value) =>
                      setForm((prev) => ({...prev, webhookChannelId: value}))
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {webhookChannels.map((channel) => (
                        <SelectItem key={channel.id} value={String(channel.id)}>
                          {channel.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}

              <div className='flex items-center justify-between rounded-lg border px-4 py-3'>
                <div>
                  <div className='font-medium'>{t('fields.recovery')}</div>
                  <div className='text-xs text-muted-foreground'>
                    {t('recoveryDesc')}
                  </div>
                </div>
                <Switch
                  checked={form.sendRecovery}
                  onCheckedChange={(checked) =>
                    setForm((prev) => ({...prev, sendRecovery: checked}))
                  }
                />
              </div>
            </div>

            <div className='flex items-center justify-end gap-2'>
              {editingPolicyId !== null ? (
                <Button variant='outline' onClick={resetPolicyForm}>
                  <X className='mr-2 h-4 w-4' />
                  {t('cancelEdit')}
                </Button>
              ) : null}
              <Button
                onClick={() => void handleSubmitPolicy()}
                disabled={savingPolicy}
              >
                {editingPolicyId === null ? (
                  <Plus className='mr-2 h-4 w-4' />
                ) : (
                  <Save className='mr-2 h-4 w-4' />
                )}
                {editingPolicyId === null ? t('createSubmit') : t('saveSubmit')}
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>

      <Dialog open={emailDialogOpen} onOpenChange={setEmailDialogOpen}>
        <DialogContent className='max-w-4xl'>
          <DialogHeader>
            <DialogTitle>{t('channel.dialogTitle')}</DialogTitle>
            <DialogDescription>{t('channel.dialogSubtitle')}</DialogDescription>
          </DialogHeader>

          <div className='grid gap-4 xl:grid-cols-[0.95fr_1.05fr]'>
            <Card>
              <CardHeader>
                <CardTitle>{t('channel.savedConfigs')}</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('channel.columns.name')}</TableHead>
                      <TableHead>{t('channel.columns.from')}</TableHead>
                      <TableHead>{t('channel.columns.recipients')}</TableHead>
                      <TableHead className='text-right'>
                        {rootT('actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {emailChannels.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={4}
                          className='text-center text-muted-foreground'
                        >
                          {t('channel.empty')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      emailChannels.map((channel) => {
                        const config = getEmailConfig(channel);
                        return (
                          <TableRow key={channel.id}>
                            <TableCell>{channel.name}</TableCell>
                            <TableCell>{config?.from || '-'}</TableCell>
                            <TableCell className='max-w-[220px] truncate'>
                              {(config?.recipients || []).join(', ')}
                            </TableCell>
                            <TableCell>
                              <div className='flex items-center justify-end gap-2'>
                                <Button
                                  variant='ghost'
                                  size='icon'
                                  onClick={() =>
                                    setEmailChannelForm(
                                      createEmailChannelFormFromChannel(
                                        channel,
                                      ),
                                    )
                                  }
                                >
                                  <Pencil className='h-4 w-4' />
                                </Button>
                                <Button
                                  variant='ghost'
                                  size='icon'
                                  onClick={() =>
                                    void handleTestEmailChannel(channel.id)
                                  }
                                  disabled={
                                    testingEmailChannelId === channel.id
                                  }
                                >
                                  <Send className='h-4 w-4' />
                                </Button>
                              </div>
                            </TableCell>
                          </TableRow>
                        );
                      })
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>
                  {emailChannelForm.id === null
                    ? t('channel.createTitle')
                    : t('channel.editTitle')}
                </CardTitle>
              </CardHeader>
              <CardContent className='space-y-4'>
                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.name')}</Label>
                    <Input
                      value={emailChannelForm.name}
                      onChange={(event) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          name: event.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.protocol')}</Label>
                    <Select
                      value={emailChannelForm.protocol}
                      onValueChange={(value) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          protocol: value,
                        }))
                      }
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='smtp'>SMTP</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.security')}</Label>
                    <Select
                      value={emailChannelForm.security}
                      onValueChange={(value) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          security: value as EmailChannelFormState['security'],
                        }))
                      }
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='ssl'>SSL/TLS</SelectItem>
                        <SelectItem value='starttls'>STARTTLS</SelectItem>
                        <SelectItem value='none'>None</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className='flex items-end gap-2'>
                    <div className='space-y-2'>
                      <Label>{t('channel.fields.enabled')}</Label>
                      <div>
                        <Switch
                          checked={emailChannelForm.enabled}
                          onCheckedChange={(checked) =>
                            setEmailChannelForm((prev) => ({
                              ...prev,
                              enabled: checked,
                            }))
                          }
                        />
                      </div>
                    </div>
                  </div>
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.host')}</Label>
                    <Input
                      value={emailChannelForm.host}
                      onChange={(event) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          host: event.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.port')}</Label>
                    <Input
                      type='number'
                      value={emailChannelForm.port}
                      onChange={(event) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          port: event.target.value,
                        }))
                      }
                    />
                  </div>
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.username')}</Label>
                    <Input
                      value={emailChannelForm.username}
                      onChange={(event) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          username: event.target.value,
                        }))
                      }
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label>{t('channel.fields.password')}</Label>
                    <Input
                      type='password'
                      value={emailChannelForm.password}
                      onChange={(event) =>
                        setEmailChannelForm((prev) => ({
                          ...prev,
                          password: event.target.value,
                        }))
                      }
                    />
                  </div>
                </div>

                <div className='space-y-2'>
                  <Label>{t('channel.fields.from')}</Label>
                  <Input
                    value={emailChannelForm.from}
                    onChange={(event) =>
                      setEmailChannelForm((prev) => ({
                        ...prev,
                        from: event.target.value,
                      }))
                    }
                  />
                </div>

                <div className='space-y-2'>
                  <Label>{t('channel.fields.recipients')}</Label>
                  <Textarea
                    value={emailChannelForm.recipients}
                    onChange={(event) =>
                      setEmailChannelForm((prev) => ({
                        ...prev,
                        recipients: event.target.value,
                      }))
                    }
                    placeholder={t('channel.placeholders.recipients')}
                    rows={3}
                  />
                </div>

                <div className='space-y-2'>
                  <Label>{t('channel.fields.description')}</Label>
                  <Textarea
                    value={emailChannelForm.description}
                    onChange={(event) =>
                      setEmailChannelForm((prev) => ({
                        ...prev,
                        description: event.target.value,
                      }))
                    }
                    rows={2}
                  />
                </div>

                <div className='flex items-center justify-end gap-2'>
                  <Button
                    variant='outline'
                    onClick={() =>
                      setEmailChannelForm(createDefaultEmailChannelForm())
                    }
                  >
                    {t('channel.reset')}
                  </Button>
                  {emailChannelForm.id !== null ? (
                    <Button
                      variant='outline'
                      onClick={() =>
                        void handleTestEmailChannel(
                          emailChannelForm.id as number,
                        )
                      }
                      disabled={testingEmailChannelId === emailChannelForm.id}
                    >
                      <Send className='mr-2 h-4 w-4' />
                      {t('channel.test')}
                    </Button>
                  ) : null}
                  <Button
                    onClick={() => void handleSaveEmailChannel()}
                    disabled={savingEmailChannel}
                  >
                    <Save className='mr-2 h-4 w-4' />
                    {emailChannelForm.id === null
                      ? t('channel.createSubmit')
                      : t('channel.saveSubmit')}
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog
        open={historyOpen}
        onOpenChange={(open) => {
          setHistoryOpen(open);
          if (!open) {
            setHistoryPolicy(null);
            setHistoryData(EMPTY_HISTORY);
          }
        }}
      >
        <DialogContent className='max-w-4xl'>
          <DialogHeader>
            <DialogTitle>
              {legacyT('history.title', {name: historyPolicy?.name || '-'})}
            </DialogTitle>
            <DialogDescription>
              {legacyT('history.subtitle', {
                total: historyData.total,
                generatedAt: formatDateTime(historyData.generated_at),
              })}
            </DialogDescription>
          </DialogHeader>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{legacyT('history.columns.channel')}</TableHead>
                <TableHead>{legacyT('history.columns.event')}</TableHead>
                <TableHead>{legacyT('history.columns.status')}</TableHead>
                <TableHead>{legacyT('history.columns.responseCode')}</TableHead>
                <TableHead>{legacyT('history.columns.sentAt')}</TableHead>
                <TableHead>{legacyT('history.columns.error')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {historyLoading ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-center text-muted-foreground'
                  >
                    {rootT('loading')}
                  </TableCell>
                </TableRow>
              ) : historyData.deliveries.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-center text-muted-foreground'
                  >
                    {legacyT('history.empty')}
                  </TableCell>
                </TableRow>
              ) : (
                historyData.deliveries.map((delivery: NotificationDelivery) => (
                  <TableRow key={delivery.id}>
                    <TableCell>{delivery.channel_name || '-'}</TableCell>
                    <TableCell>
                      {delivery.event_type === 'resolved'
                        ? legacyT('history.events.resolved')
                        : delivery.event_type === 'test'
                          ? legacyT('history.events.test')
                          : legacyT('history.events.firing')}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={resolveDeliveryStatusVariant(delivery.status)}
                      >
                        {delivery.status === 'sent'
                          ? legacyT('history.statuses.sent')
                          : delivery.status === 'failed'
                            ? legacyT('history.statuses.failed')
                            : delivery.status === 'sending'
                              ? legacyT('history.statuses.sending')
                              : delivery.status === 'retrying'
                                ? legacyT('history.statuses.retrying')
                                : legacyT('history.statuses.pending')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {delivery.response_status_code || '-'}
                    </TableCell>
                    <TableCell>{formatDateTime(delivery.sent_at)}</TableCell>
                    <TableCell className='max-w-[260px] truncate'>
                      {delivery.last_error || '-'}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </DialogContent>
      </Dialog>
    </div>
  );
}

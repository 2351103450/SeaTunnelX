'use client';

import {useCallback, useEffect, useMemo, useState} from 'react';
import {useTranslations} from 'next-intl';
import {toast} from 'sonner';
import {RefreshCw} from 'lucide-react';
import services from '@/lib/services';
import type {ClusterInfo} from '@/lib/services/cluster';
import type {
  AlertEvent,
  AlertStats,
  AlertStatus,
} from '@/lib/services/monitoring';
import {Button} from '@/components/ui/button';
import {Badge} from '@/components/ui/badge';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

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

function resolveBadgeVariant(status: AlertStatus):
  | 'default'
  | 'secondary'
  | 'outline'
  | 'destructive' {
  if (status === 'firing') {
    return 'destructive';
  }
  if (status === 'acknowledged') {
    return 'secondary';
  }
  return 'outline';
}

export function MonitoringAlertsCenter() {
  const t = useTranslations('monitoringCenter');

  const [clusterOptions, setClusterOptions] = useState<ClusterInfo[]>([]);
  const [clusterFilter, setClusterFilter] = useState<string>('all');
  const [statusFilter, setStatusFilter] = useState<string>('all');

  const [alerts, setAlerts] = useState<AlertEvent[]>([]);
  const [stats, setStats] = useState<AlertStats | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [actingEventId, setActingEventId] = useState<number | null>(null);

  const statusLabelMap = useMemo(
    () => ({
      firing: t('alertStatuses.firing'),
      acknowledged: t('alertStatuses.acknowledged'),
      silenced: t('alertStatuses.silenced'),
    }),
    [t],
  );

  const severityLabelMap = useMemo(
    () => ({
      warning: t('alertSeverity.warning'),
      critical: t('alertSeverity.critical'),
    }),
    [t],
  );

  const loadClusters = useCallback(async () => {
    try {
      const data = await services.cluster.getClusters({
        current: 1,
        size: 200,
      });
      setClusterOptions(data.clusters || []);
    } catch {
      setClusterOptions([]);
    }
  }, []);

  const loadAlerts = useCallback(async () => {
    setLoading(true);
    try {
      const result = await services.monitoring.getAlertsSafe({
        cluster_id:
          clusterFilter === 'all' ? undefined : Number.parseInt(clusterFilter, 10),
        status: statusFilter === 'all' ? undefined : (statusFilter as AlertStatus),
        page: 1,
        page_size: 200,
      });

      if (!result.success || !result.data) {
        toast.error(result.error || t('alerts.loadError'));
        setAlerts([]);
        setStats(null);
        return;
      }

      setAlerts(result.data.alerts || []);
      setStats(result.data.stats || null);
    } finally {
      setLoading(false);
    }
  }, [clusterFilter, statusFilter, t]);

  useEffect(() => {
    loadClusters();
  }, [loadClusters]);

  useEffect(() => {
    loadAlerts();
  }, [loadAlerts]);

  const handleAcknowledge = async (alert: AlertEvent) => {
    setActingEventId(alert.event_id);
    try {
      const result = await services.monitoring.acknowledgeAlertSafe(alert.event_id, {});
      if (!result.success) {
        toast.error(result.error || t('alerts.ackError'));
        return;
      }
      toast.success(t('alerts.ackSuccess'));
      await loadAlerts();
    } finally {
      setActingEventId(null);
    }
  };

  const handleSilence = async (alert: AlertEvent) => {
    setActingEventId(alert.event_id);
    try {
      const result = await services.monitoring.silenceAlertSafe(alert.event_id, {
        duration_minutes: 30,
      });
      if (!result.success) {
        toast.error(result.error || t('alerts.silenceError'));
        return;
      }
      toast.success(t('alerts.silenceSuccess'));
      await loadAlerts();
    } finally {
      setActingEventId(null);
    }
  };

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('alerts.title')}</CardTitle>
          <div className='flex flex-col lg:flex-row gap-2 lg:items-center lg:justify-between'>
            <div className='flex flex-col md:flex-row gap-2'>
              <div className='w-full md:w-56'>
                <Select value={clusterFilter} onValueChange={setClusterFilter}>
                  <SelectTrigger>
                    <SelectValue placeholder={t('alerts.clusterFilter')} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>{t('alerts.allClusters')}</SelectItem>
                    {clusterOptions.map((cluster) => (
                      <SelectItem key={cluster.id} value={String(cluster.id)}>
                        {cluster.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className='w-full md:w-56'>
                <Select value={statusFilter} onValueChange={setStatusFilter}>
                  <SelectTrigger>
                    <SelectValue placeholder={t('alerts.statusFilter')} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>{t('alerts.allStatus')}</SelectItem>
                    <SelectItem value='firing'>{statusLabelMap.firing}</SelectItem>
                    <SelectItem value='acknowledged'>
                      {statusLabelMap.acknowledged}
                    </SelectItem>
                    <SelectItem value='silenced'>
                      {statusLabelMap.silenced}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className='flex items-center gap-2'>
              {stats ? (
                <>
                  <Badge variant='destructive'>{`${statusLabelMap.firing}: ${stats.firing}`}</Badge>
                  <Badge variant='secondary'>{`${statusLabelMap.acknowledged}: ${stats.acknowledged}`}</Badge>
                  <Badge variant='outline'>{`${statusLabelMap.silenced}: ${stats.silenced}`}</Badge>
                </>
              ) : null}
              <Button variant='outline' onClick={loadAlerts}>
                <RefreshCw className='h-4 w-4 mr-2' />
                {t('refresh')}
              </Button>
            </div>
          </div>
        </CardHeader>

        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('alerts.cluster')}</TableHead>
                <TableHead>{t('alerts.eventType')}</TableHead>
                <TableHead>{t('alerts.severity')}</TableHead>
                <TableHead>{t('alerts.status')}</TableHead>
                <TableHead>{t('alerts.host')}</TableHead>
                <TableHead>{t('alerts.process')}</TableHead>
                <TableHead>{t('alerts.eventTime')}</TableHead>
                <TableHead>{t('actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={8} className='text-center text-muted-foreground'>
                    {t('loading')}
                  </TableCell>
                </TableRow>
              ) : !alerts.length ? (
                <TableRow>
                  <TableCell colSpan={8} className='text-center text-muted-foreground'>
                    {t('alerts.noAlerts')}
                  </TableCell>
                </TableRow>
              ) : (
                alerts.map((alert) => (
                  <TableRow key={alert.event_id}>
                    <TableCell>{alert.cluster_name}</TableCell>
                    <TableCell>{alert.event_type}</TableCell>
                    <TableCell>
                      {severityLabelMap[alert.severity] || alert.severity}
                    </TableCell>
                    <TableCell>
                      <Badge variant={resolveBadgeVariant(alert.status)}>
                        {statusLabelMap[alert.status] || alert.status}
                      </Badge>
                    </TableCell>
                    <TableCell>{`${alert.hostname} (${alert.ip})`}</TableCell>
                    <TableCell>{`${alert.process_name} (${alert.pid})`}</TableCell>
                    <TableCell>{formatDateTime(alert.created_at)}</TableCell>
                    <TableCell>
                      <div className='flex flex-wrap gap-2'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => handleAcknowledge(alert)}
                          disabled={
                            alert.status !== 'firing' ||
                            actingEventId === alert.event_id
                          }
                        >
                          {t('alerts.ack')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => handleSilence(alert)}
                          disabled={
                            alert.status !== 'firing' ||
                            actingEventId === alert.event_id
                          }
                        >
                          {t('alerts.silence30m')}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}

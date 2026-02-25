'use client';

import {useCallback, useEffect, useMemo, useState} from 'react';
import {useTranslations} from 'next-intl';
import {toast} from 'sonner';
import services from '@/lib/services';
import type {
  NotificationChannel,
  NotificationChannelType,
  UpsertNotificationChannelRequest,
} from '@/lib/services/monitoring';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Label} from '@/components/ui/label';
import {Switch} from '@/components/ui/switch';
import {Textarea} from '@/components/ui/textarea';
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

const DEFAULT_FORM: UpsertNotificationChannelRequest = {
  name: '',
  type: 'webhook',
  endpoint: '',
  secret: '',
  description: '',
  enabled: true,
};

export function MonitoringIntegrationsPanel() {
  const t = useTranslations('monitoringCenter');

  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [creating, setCreating] = useState<boolean>(false);
  const [updatingId, setUpdatingId] = useState<number | null>(null);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const [form, setForm] = useState<UpsertNotificationChannelRequest>(DEFAULT_FORM);

  const typeLabelMap = useMemo(
    () => ({
      webhook: t('phase3.channelTypes.webhook'),
      email: t('phase3.channelTypes.email'),
      wecom: t('phase3.channelTypes.wecom'),
      dingtalk: t('phase3.channelTypes.dingtalk'),
      feishu: t('phase3.channelTypes.feishu'),
    }),
    [t],
  );

  const loadChannels = useCallback(async () => {
    setLoading(true);
    try {
      const result = await services.monitoring.listNotificationChannelsSafe();
      if (!result.success || !result.data) {
        toast.error(result.error || t('phase3.channelLoadError'));
        setChannels([]);
        return;
      }
      setChannels(result.data.channels || []);
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadChannels();
  }, [loadChannels]);

  const handleCreate = async () => {
    if (!form.name.trim() || !form.endpoint.trim()) {
      toast.error(t('phase3.channelFormInvalid'));
      return;
    }

    setCreating(true);
    try {
      const result = await services.monitoring.createNotificationChannelSafe({
        ...form,
        name: form.name.trim(),
        endpoint: form.endpoint.trim(),
      });
      if (!result.success) {
        toast.error(result.error || t('phase3.channelCreateError'));
        return;
      }
      toast.success(t('phase3.channelCreateSuccess'));
      setForm(DEFAULT_FORM);
      await loadChannels();
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (channelId: number) => {
    setDeletingId(channelId);
    try {
      const result = await services.monitoring.deleteNotificationChannelSafe(channelId);
      if (!result.success) {
        toast.error(result.error || t('phase3.channelDeleteError'));
        return;
      }
      toast.success(t('phase3.channelDeleteSuccess'));
      await loadChannels();
    } finally {
      setDeletingId(null);
    }
  };

  const handleToggleEnabled = async (channel: NotificationChannel, enabled: boolean) => {
    setUpdatingId(channel.id);
    try {
      const result = await services.monitoring.updateNotificationChannelSafe(channel.id, {
        name: channel.name,
        type: channel.type,
        endpoint: channel.endpoint,
        secret: channel.secret,
        description: channel.description,
        enabled,
      });
      if (!result.success) {
        toast.error(result.error || t('phase3.channelCreateError'));
        return;
      }
      await loadChannels();
    } finally {
      setUpdatingId(null);
    }
  };

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle>{t('phase3.channelCreateTitle')}</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <div className='grid grid-cols-1 md:grid-cols-2 gap-4'>
            <div className='space-y-2'>
              <Label>{t('phase3.channelName')}</Label>
              <Input
                value={form.name}
                onChange={(event) =>
                  setForm((prev) => ({...prev, name: event.target.value}))
                }
              />
            </div>
            <div className='space-y-2'>
              <Label>{t('phase3.channelType')}</Label>
              <Select
                value={form.type}
                onValueChange={(value) =>
                  setForm((prev) => ({
                    ...prev,
                    type: value as NotificationChannelType,
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='webhook'>
                    {typeLabelMap.webhook}
                  </SelectItem>
                  <SelectItem value='email'>{typeLabelMap.email}</SelectItem>
                  <SelectItem value='wecom'>{typeLabelMap.wecom}</SelectItem>
                  <SelectItem value='dingtalk'>
                    {typeLabelMap.dingtalk}
                  </SelectItem>
                  <SelectItem value='feishu'>{typeLabelMap.feishu}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className='space-y-2'>
            <Label>{t('phase3.channelEndpoint')}</Label>
            <Input
              value={form.endpoint}
              onChange={(event) =>
                setForm((prev) => ({...prev, endpoint: event.target.value}))
              }
            />
          </div>

          <div className='space-y-2'>
            <Label>{t('phase3.channelSecret')}</Label>
            <Input
              value={form.secret || ''}
              onChange={(event) =>
                setForm((prev) => ({...prev, secret: event.target.value}))
              }
            />
          </div>

          <div className='space-y-2'>
            <Label>{t('phase3.channelDescription')}</Label>
            <Textarea
              rows={3}
              value={form.description || ''}
              onChange={(event) =>
                setForm((prev) => ({...prev, description: event.target.value}))
              }
            />
          </div>

          <div className='flex items-center gap-3'>
            <Switch
              checked={form.enabled ?? true}
              onCheckedChange={(checked) =>
                setForm((prev) => ({...prev, enabled: checked}))
              }
            />
            <Label>{t('phase3.channelEnabled')}</Label>
          </div>

          <Button onClick={handleCreate} disabled={creating}>
            {t('phase3.channelCreate')}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('phase3.channelListTitle')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('phase3.channelName')}</TableHead>
                <TableHead>{t('phase3.channelType')}</TableHead>
                <TableHead>{t('phase3.channelEndpoint')}</TableHead>
                <TableHead>{t('phase3.channelEnabled')}</TableHead>
                <TableHead>{t('actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={5} className='text-center text-muted-foreground'>
                    {t('loading')}
                  </TableCell>
                </TableRow>
              ) : !channels.length ? (
                <TableRow>
                  <TableCell colSpan={5} className='text-center text-muted-foreground'>
                    {t('phase3.channelEmpty')}
                  </TableCell>
                </TableRow>
              ) : (
                channels.map((channel) => (
                  <TableRow key={channel.id}>
                    <TableCell>{channel.name}</TableCell>
                    <TableCell>
                      {typeLabelMap[channel.type as NotificationChannelType] ||
                        channel.type}
                    </TableCell>
                    <TableCell className='max-w-[380px] break-all'>
                      {channel.endpoint}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={channel.enabled}
                        disabled={updatingId === channel.id}
                        onCheckedChange={(checked) =>
                          handleToggleEnabled(channel, checked)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Button
                        size='sm'
                        variant='destructive'
                        disabled={deletingId === channel.id}
                        onClick={() => handleDelete(channel.id)}
                      >
                        {t('phase3.delete')}
                      </Button>
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

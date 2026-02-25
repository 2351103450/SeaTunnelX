'use client';

import {useEffect, useMemo, useState} from 'react';
import {useTranslations} from 'next-intl';
import {useLocale} from '@/lib/i18n';
import {Button} from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {ExternalLink, Loader2, RefreshCw} from 'lucide-react';

type TimeRange = 'now-1h' | 'now-6h' | 'now-24h' | 'now-7d';
type RefreshInterval = 'off' | '15s' | '30s' | '1m';

function resolveDashboardUID(locale: string): string {
  return locale === 'zh' ? 'seatunnel-overview-zh' : 'seatunnel-overview-en';
}

function resolveDashboardSlug(locale: string): string {
  return locale === 'zh'
    ? 'seatunnelx-shen-du-jian-kong'
    : 'seatunnelx-deep-monitoring';
}

function buildGrafanaProxyDashboardURL(
  locale: string,
  timeFrom: TimeRange,
  refresh: RefreshInterval,
  embedMode: boolean,
): string {
  const uid = resolveDashboardUID(locale);
  const slug = resolveDashboardSlug(locale);
  const path = `/api/v1/monitoring/proxy/grafana/d/${uid}/${slug}`;

  const search = new URLSearchParams({
    orgId: '1',
    from: timeFrom,
    to: 'now',
    kiosk: 'tv',
  });
  if (refresh !== 'off') {
    search.set('refresh', refresh);
  }
  if (embedMode) {
    search.set('theme', 'dark');
  }

  return `${path}?${search.toString()}`;
}

export function MonitoringOverview() {
  const t = useTranslations('monitoringCenter');
  const {locale} = useLocale();

  const [timeRange, setTimeRange] = useState<TimeRange>('now-6h');
  const [refreshInterval, setRefreshInterval] =
    useState<RefreshInterval>('15s');
  const [iframeKey, setIframeKey] = useState(1);
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);

  const dashboardURL = useMemo(
    () =>
      buildGrafanaProxyDashboardURL(locale, timeRange, refreshInterval, false),
    [locale, timeRange, refreshInterval],
  );
  const embedURL = useMemo(
    () =>
      buildGrafanaProxyDashboardURL(locale, timeRange, refreshInterval, true),
    [locale, timeRange, refreshInterval],
  );

  useEffect(() => {
    setLoaded(false);
    setLoadFailed(false);
    const timer = setTimeout(() => {
      setLoadFailed(true);
    }, 15000);
    return () => clearTimeout(timer);
  }, [embedURL, iframeKey]);

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader className='space-y-3'>
          <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between gap-3'>
            <CardTitle>{t('grafana.title')}</CardTitle>
            <div className='flex items-center gap-2 flex-wrap'>
              <Button
                variant='outline'
                onClick={() => setIframeKey((v) => v + 1)}
                className='shrink-0'
              >
                <RefreshCw className='h-4 w-4 mr-2' />
                {t('grafana.reload')}
              </Button>
              <Button asChild variant='outline' className='shrink-0'>
                <a href={dashboardURL} target='_blank' rel='noreferrer'>
                  <ExternalLink className='h-4 w-4 mr-2' />
                  {t('grafana.open')}
                </a>
              </Button>
            </div>
          </div>

          <div className='flex flex-col md:flex-row gap-2 md:items-center'>
            <div className='w-full md:w-56'>
              <Select
                value={timeRange}
                onValueChange={(value) => setTimeRange(value as TimeRange)}
              >
                <SelectTrigger>
                  <SelectValue placeholder={t('grafana.timeRange.label')} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='now-1h'>
                    {t('grafana.timeRange.last1h')}
                  </SelectItem>
                  <SelectItem value='now-6h'>
                    {t('grafana.timeRange.last6h')}
                  </SelectItem>
                  <SelectItem value='now-24h'>
                    {t('grafana.timeRange.last24h')}
                  </SelectItem>
                  <SelectItem value='now-7d'>
                    {t('grafana.timeRange.last7d')}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className='w-full md:w-56'>
              <Select
                value={refreshInterval}
                onValueChange={(value) =>
                  setRefreshInterval(value as RefreshInterval)
                }
              >
                <SelectTrigger>
                  <SelectValue
                    placeholder={t('grafana.refreshInterval.label')}
                  />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='off'>
                    {t('grafana.refreshInterval.off')}
                  </SelectItem>
                  <SelectItem value='15s'>15s</SelectItem>
                  <SelectItem value='30s'>30s</SelectItem>
                  <SelectItem value='1m'>1m</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardHeader>

        <CardContent className='p-0'>
          <div className='relative min-h-[760px] border-t bg-muted/20'>
            {!loaded && !loadFailed ? (
              <div className='absolute inset-0 z-10 flex items-center justify-center bg-background/70 backdrop-blur-sm'>
                <div className='flex items-center gap-2 text-sm text-muted-foreground'>
                  <Loader2 className='h-4 w-4 animate-spin' />
                  {t('grafana.loading')}
                </div>
              </div>
            ) : null}

            {loadFailed ? (
              <div className='absolute inset-0 z-20 flex flex-col items-center justify-center gap-3 bg-background/80 backdrop-blur-sm px-6 text-center'>
                <div className='text-sm text-muted-foreground'>
                  {t('grafana.loadFailed')}
                </div>
                <Button
                  variant='outline'
                  onClick={() => setIframeKey((v) => v + 1)}
                >
                  {t('grafana.retry')}
                </Button>
              </div>
            ) : null}

            <iframe
              key={iframeKey}
              title='Seatunnel Grafana Dashboard'
              src={embedURL}
              className='w-full min-h-[760px] h-[calc(100vh-230px)] border-0 rounded-b-xl'
              sandbox='allow-scripts allow-same-origin allow-forms allow-popups allow-downloads'
              referrerPolicy='strict-origin-when-cross-origin'
              loading='lazy'
              onLoad={() => {
                setLoaded(true);
                setLoadFailed(false);
              }}
            />

            <div className='pointer-events-none absolute top-0 left-0 right-0 h-8 bg-background/95' />
            <div className='pointer-events-none absolute bottom-0 left-0 right-0 h-2 bg-background/95' />
            <div className='pointer-events-none absolute top-0 bottom-0 left-0 w-1 bg-background/95' />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

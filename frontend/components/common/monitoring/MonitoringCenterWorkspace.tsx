'use client';

import {useEffect, useMemo, useState} from 'react';
import {useTranslations} from 'next-intl';
import {useSearchParams} from 'next/navigation';
import {Activity} from 'lucide-react';
import {Tabs, TabsContent, TabsList, TabsTrigger} from '@/components/ui/tabs';
import {MonitoringAlertsCenter} from './MonitoringAlertsCenter';
import {MonitoringRulesPanel} from './MonitoringRulesPanel';
import {MonitoringIntegrationsPanel} from './MonitoringIntegrationsPanel';

type MonitoringTab = 'alerts' | 'rules' | 'integrations';

function resolveTab(tab: string | null): MonitoringTab {
  if (tab === 'alerts') {
    return 'alerts';
  }
  if (tab === 'rules') {
    return 'rules';
  }
  if (tab === 'integrations' || tab === 'notifications') {
    return 'integrations';
  }
  // 默认聚焦告警中心，而非总览看板。
  return 'alerts';
}

export function MonitoringCenterWorkspace() {
  const t = useTranslations('monitoringCenter');
  const searchParams = useSearchParams();

  const initialTab = useMemo(
    () => resolveTab(searchParams.get('tab')),
    [searchParams],
  );
  const [activeTab, setActiveTab] = useState<MonitoringTab>(initialTab);

  useEffect(() => {
    setActiveTab(resolveTab(searchParams.get('tab')));
  }, [searchParams]);

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-3'>
        <Activity className='h-8 w-8 shrink-0 text-primary' />
        <div>
          <h1 className='text-2xl font-bold tracking-tight'>{t('title')}</h1>
          <p className='text-muted-foreground mt-1'>{t('subtitle')}</p>
        </div>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as MonitoringTab)}
      >
        <TabsList className='grid w-full grid-cols-3 gap-1 md:grid-cols-3'>
          <TabsTrigger value='alerts'>{t('tabs.alerts')}</TabsTrigger>
          <TabsTrigger value='rules'>{t('tabs.rules')}</TabsTrigger>
          <TabsTrigger value='integrations'>
            {t('tabs.integrations')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value='alerts' className='mt-4'>
          <MonitoringAlertsCenter />
        </TabsContent>

        <TabsContent value='rules' className='mt-4'>
          <MonitoringRulesPanel />
        </TabsContent>

        <TabsContent value='integrations' className='mt-4'>
          <MonitoringIntegrationsPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}

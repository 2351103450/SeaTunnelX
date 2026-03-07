/**
 * Monitoring Center Page
 * 监控中心页面
 */

import {Suspense} from 'react';
import {Metadata} from 'next';
import {MonitoringCenterWorkspace} from '@/components/common/monitoring';

export const metadata: Metadata = {
  title: '告警中心',
};

export default function MonitoringPage() {
  return (
    <Suspense>
      <MonitoringCenterWorkspace />
    </Suspense>
  );
}

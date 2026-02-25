/**
 * Monitoring Center Page
 * 监控中心页面
 */

import {Suspense} from 'react';
import {Metadata} from 'next';
import {MonitoringCenterWorkspace} from '@/components/common/monitoring';

export const metadata: Metadata = {
  title: '监控中心',
};

export default function MonitoringPage() {
  return (
    <div className='container max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8'>
      <Suspense>
        <MonitoringCenterWorkspace />
      </Suspense>
    </div>
  );
}
